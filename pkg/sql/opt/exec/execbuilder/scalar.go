// Copyright 2018 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package execbuilder

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/sql/opt"
	"github.com/cockroachdb/cockroach/pkg/sql/opt/exec"
	"github.com/cockroachdb/cockroach/pkg/sql/opt/memo"
	"github.com/cockroachdb/cockroach/pkg/sql/opt/norm"
	"github.com/cockroachdb/cockroach/pkg/sql/opt/xform"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/builtins/builtinsregistry"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/eval"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree/treebin"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree/treecmp"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/volatility"
	"github.com/cockroachdb/cockroach/pkg/sql/types"
	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/redact"
)

type buildScalarCtx struct {
	ivh tree.IndexedVarHelper

	// ivarMap is a map from opt.ColumnID to the index of an IndexedVar.
	// If a ColumnID is not in the map, it cannot appear in the expression.
	ivarMap opt.ColMap
}

type buildFunc func(b *Builder, ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error)

var scalarBuildFuncMap [opt.NumOperators]buildFunc

func init() {
	// This code is not inline to avoid an initialization loop error (some of
	// the functions depend on scalarBuildFuncMap which in turn depends on the
	// functions).
	scalarBuildFuncMap = [opt.NumOperators]buildFunc{
		opt.VariableOp:       (*Builder).buildVariable,
		opt.ConstOp:          (*Builder).buildTypedExpr,
		opt.NullOp:           (*Builder).buildNull,
		opt.PlaceholderOp:    (*Builder).buildTypedExpr,
		opt.TupleOp:          (*Builder).buildTuple,
		opt.FunctionOp:       (*Builder).buildFunction,
		opt.CaseOp:           (*Builder).buildCase,
		opt.CastOp:           (*Builder).buildCast,
		opt.AssignmentCastOp: (*Builder).buildAssignmentCast,
		opt.CoalesceOp:       (*Builder).buildCoalesce,
		opt.ColumnAccessOp:   (*Builder).buildColumnAccess,
		opt.ArrayOp:          (*Builder).buildArray,
		opt.AnyOp:            (*Builder).buildAny,
		opt.AnyScalarOp:      (*Builder).buildAnyScalar,
		opt.IndirectionOp:    (*Builder).buildIndirection,
		opt.CollateOp:        (*Builder).buildCollate,
		opt.ArrayFlattenOp:   (*Builder).buildArrayFlatten,
		opt.IfErrOp:          (*Builder).buildIfErr,

		// Item operators.
		opt.ProjectionsItemOp:  (*Builder).buildItem,
		opt.AggregationsItemOp: (*Builder).buildItem,

		// Subquery operators.
		opt.ExistsOp:   (*Builder).buildExistsSubquery,
		opt.SubqueryOp: (*Builder).buildSubquery,

		// User-defined functions.
		opt.UDFOp: (*Builder).buildUDF,
	}

	for _, op := range opt.BoolOperators {
		if scalarBuildFuncMap[op] == nil {
			scalarBuildFuncMap[op] = (*Builder).buildBoolean
		}
	}

	for _, op := range opt.ComparisonOperators {
		scalarBuildFuncMap[op] = (*Builder).buildComparison
	}

	for _, op := range opt.BinaryOperators {
		scalarBuildFuncMap[op] = (*Builder).buildBinary
	}

	for _, op := range opt.UnaryOperators {
		scalarBuildFuncMap[op] = (*Builder).buildUnary
	}
}

// buildScalar converts a scalar expression to a TypedExpr. Variables are mapped
// according to ctx.
func (b *Builder) buildScalar(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	fn := scalarBuildFuncMap[scalar.Op()]
	if fn == nil {
		return nil, errors.AssertionFailedf("unsupported op %s", redact.Safe(scalar.Op()))
	}
	return fn(b, ctx, scalar)
}

func (b *Builder) buildScalarWithMap(
	colMap opt.ColMap, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	ctx := buildScalarCtx{
		ivh:     tree.MakeIndexedVarHelper(nil /* container */, numOutputColsInMap(colMap)),
		ivarMap: colMap,
	}
	return b.buildScalar(&ctx, scalar)
}

func (b *Builder) buildTypedExpr(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	return scalar.Private().(tree.TypedExpr), nil
}

func (b *Builder) buildNull(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	retypedNull, ok := eval.ReType(tree.DNull, scalar.DataType())
	if !ok {
		return nil, errors.AssertionFailedf("failed to retype NULL to %s", scalar.DataType())
	}
	return retypedNull, nil
}

func (b *Builder) buildVariable(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	return b.indexedVar(ctx, b.mem.Metadata(), *scalar.Private().(*opt.ColumnID)), nil
}

func (b *Builder) indexedVar(
	ctx *buildScalarCtx, md *opt.Metadata, colID opt.ColumnID,
) tree.TypedExpr {
	idx, ok := ctx.ivarMap.Get(int(colID))
	if !ok {
		panic(errors.AssertionFailedf("cannot map variable %d to an indexed var", redact.Safe(colID)))
	}
	return ctx.ivh.IndexedVarWithType(idx, md.ColumnMeta(colID).Type)
}

func (b *Builder) buildTuple(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	if memo.CanExtractConstTuple(scalar) {
		return memo.ExtractConstDatum(scalar), nil
	}

	tup := scalar.(*memo.TupleExpr)
	typedExprs := make(tree.Exprs, len(tup.Elems))
	var err error
	for i, elem := range tup.Elems {
		typedExprs[i], err = b.buildScalar(ctx, elem)
		if err != nil {
			return nil, err
		}
	}
	return tree.NewTypedTuple(tup.Typ, typedExprs), nil
}

func (b *Builder) buildBoolean(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	switch scalar.Op() {
	case opt.FiltersOp:
		if scalar.ChildCount() == 0 {
			// This can happen if the expression is not normalized (build tests).
			return tree.DBoolTrue, nil
		}
		fallthrough

	case opt.AndOp, opt.OrOp:
		expr, err := b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))
		if err != nil {
			return nil, err
		}
		for i, n := 1, scalar.ChildCount(); i < n; i++ {
			right, err := b.buildScalar(ctx, scalar.Child(i).(opt.ScalarExpr))
			if err != nil {
				return nil, err
			}
			if scalar.Op() == opt.OrOp {
				expr = tree.NewTypedOrExpr(expr, right)
			} else {
				expr = tree.NewTypedAndExpr(expr, right)
			}
		}
		return expr, nil

	case opt.NotOp:
		expr, err := b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))
		if err != nil {
			return nil, err
		}
		return tree.NewTypedNotExpr(expr), nil

	case opt.TrueOp:
		return tree.DBoolTrue, nil

	case opt.FalseOp:
		return tree.DBoolFalse, nil

	case opt.FiltersItemOp:
		return b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))

	case opt.RangeOp:
		return b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))

	case opt.IsTupleNullOp:
		expr, err := b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))
		if err != nil {
			return nil, err
		}
		return tree.NewTypedIsNullExpr(expr), nil

	case opt.IsTupleNotNullOp:
		expr, err := b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))
		if err != nil {
			return nil, err
		}
		return tree.NewTypedIsNotNullExpr(expr), nil

	default:
		panic(errors.AssertionFailedf("invalid op %s", redact.Safe(scalar.Op())))
	}
}

func (b *Builder) buildComparison(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	left, err := b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))
	if err != nil {
		return nil, err
	}
	right, err := b.buildScalar(ctx, scalar.Child(1).(opt.ScalarExpr))
	if err != nil {
		return nil, err
	}

	// When the operator is an IsOp, the right is NULL, and the left is not a
	// tuple, return the unary tree.IsNullExpr.
	if scalar.Op() == opt.IsOp && right == tree.DNull && left.ResolvedType().Family() != types.TupleFamily {
		return tree.NewTypedIsNullExpr(left), nil
	}

	// When the operator is an IsNotOp, the right is NULL, and the left is not a
	// tuple, return the unary tree.IsNotNullExpr.
	if scalar.Op() == opt.IsNotOp && right == tree.DNull && left.ResolvedType().Family() != types.TupleFamily {
		return tree.NewTypedIsNotNullExpr(left), nil
	}

	operator := opt.ComparisonOpReverseMap[scalar.Op()]
	return tree.NewTypedComparisonExpr(treecmp.MakeComparisonOperator(operator), left, right), nil
}

func (b *Builder) buildUnary(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	input, err := b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))
	if err != nil {
		return nil, err
	}
	operator := opt.UnaryOpReverseMap[scalar.Op()]
	return tree.NewTypedUnaryExpr(tree.MakeUnaryOperator(operator), input, scalar.DataType()), nil
}

func (b *Builder) buildBinary(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	left, err := b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))
	if err != nil {
		return nil, err
	}
	right, err := b.buildScalar(ctx, scalar.Child(1).(opt.ScalarExpr))
	if err != nil {
		return nil, err
	}
	operator := opt.BinaryOpReverseMap[scalar.Op()]
	return tree.NewTypedBinaryExpr(treebin.MakeBinaryOperator(operator), left, right, scalar.DataType()), nil
}

func (b *Builder) buildFunction(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	fn := scalar.(*memo.FunctionExpr)
	exprs := make(tree.TypedExprs, len(fn.Args))
	var err error
	for i := range exprs {
		exprs[i], err = b.buildScalar(ctx, fn.Args[i])
		if err != nil {
			return nil, err
		}
	}
	funcRef := b.wrapFunction(fn.Name)
	return tree.NewTypedFuncExpr(
		funcRef,
		0, /* aggQualifier */
		exprs,
		nil, /* filter */
		nil, /* windowDef */
		fn.Typ,
		fn.Properties,
		fn.Overload,
	), nil
}

func (b *Builder) buildCase(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	cas := scalar.(*memo.CaseExpr)
	input, err := b.buildScalar(ctx, cas.Input)
	if err != nil {
		return nil, err
	}

	// A searched CASE statement is represented by the optimizer with input=True.
	// The executor expects searched CASE statements to have nil inputs.
	if input == tree.DBoolTrue {
		input = nil
	}

	// Extract the list of WHEN ... THEN ... clauses.
	whensVals := make([]tree.When, len(cas.Whens))
	whens := make([]*tree.When, len(cas.Whens))
	for i, expr := range cas.Whens {
		whenExpr := expr.(*memo.WhenExpr)
		cond, err := b.buildScalar(ctx, whenExpr.Condition)
		if err != nil {
			return nil, err
		}
		val, err := b.buildScalar(ctx, whenExpr.Value)
		if err != nil {
			return nil, err
		}
		whensVals[i] = tree.When{Cond: cond, Val: val}
		whens[i] = &whensVals[i]
	}

	elseExpr, err := b.buildScalar(ctx, cas.OrElse)
	if err != nil {
		return nil, err
	}
	if elseExpr == tree.DNull {
		elseExpr = nil
	}

	return tree.NewTypedCaseExpr(input, whens, elseExpr, cas.Typ)
}

func (b *Builder) buildCast(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	cast := scalar.(*memo.CastExpr)
	input, err := b.buildScalar(ctx, cast.Input)
	if err != nil {
		return nil, err
	}
	return tree.NewTypedCastExpr(input, cast.Typ), nil
}

// buildAssignmentCast builds an AssignmentCastExpr with input i and type T into
// a built-in function call crdb_internal.assignment_cast(i, NULL::T).
func (b *Builder) buildAssignmentCast(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	cast := scalar.(*memo.AssignmentCastExpr)
	input, err := b.buildScalar(ctx, cast.Input)
	if err != nil {
		return nil, err
	}
	if cast.Typ.Family() == types.TupleFamily {
		// TODO(radu): casts to Tuple are not supported (they can't be
		// serialized for distsql). This should only happen when the input is
		// always NULL so the expression should still be valid without the cast
		// (though there could be cornercases where the type does matter).
		return input, nil
	}
	const fnName = "crdb_internal.assignment_cast"
	funcRef := b.wrapFunction(fnName)
	props, overloads := builtinsregistry.GetBuiltinProperties(fnName)
	return tree.NewTypedFuncExpr(
		funcRef,
		0, /* aggQualifier */
		tree.TypedExprs{input, tree.NewTypedCastExpr(tree.DNull, cast.Typ)},
		nil, /* filter */
		nil, /* windowDef */
		cast.Typ,
		props,
		&overloads[0],
	), nil
}

func (b *Builder) buildCoalesce(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	coalesce := scalar.(*memo.CoalesceExpr)
	exprs := make(tree.TypedExprs, len(coalesce.Args))
	var err error
	for i := range exprs {
		exprs[i], err = b.buildScalar(ctx, coalesce.Args[i])
		if err != nil {
			return nil, err
		}
	}
	return tree.NewTypedCoalesceExpr(exprs, coalesce.Typ), nil
}

func (b *Builder) buildColumnAccess(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	colAccess := scalar.(*memo.ColumnAccessExpr)
	input, err := b.buildScalar(ctx, colAccess.Input)
	if err != nil {
		return nil, err
	}
	childTyp := colAccess.Input.DataType()
	colIdx := int(colAccess.Idx)
	// Find a label if there is one. It's OK if there isn't.
	lbl := ""
	if childTyp.TupleLabels() != nil {
		lbl = childTyp.TupleLabels()[colIdx]
	}
	return tree.NewTypedColumnAccessExpr(input, tree.Name(lbl), colIdx), nil
}

func (b *Builder) buildArray(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	arr := scalar.(*memo.ArrayExpr)
	if memo.CanExtractConstDatum(scalar) {
		return memo.ExtractConstDatum(scalar), nil
	}
	exprs := make(tree.TypedExprs, len(arr.Elems))
	var err error
	for i := range exprs {
		exprs[i], err = b.buildScalar(ctx, arr.Elems[i])
		if err != nil {
			return nil, err
		}
	}
	return tree.NewTypedArray(exprs, arr.Typ), nil
}

func (b *Builder) buildAnyScalar(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	any := scalar.(*memo.AnyScalarExpr)
	left, err := b.buildScalar(ctx, any.Left)
	if err != nil {
		return nil, err
	}

	right, err := b.buildScalar(ctx, any.Right)
	if err != nil {
		return nil, err
	}

	cmp := opt.ComparisonOpReverseMap[any.Cmp]
	return tree.NewTypedComparisonExprWithSubOp(
		treecmp.MakeComparisonOperator(treecmp.Any),
		treecmp.MakeComparisonOperator(cmp),
		left,
		right,
	), nil
}

func (b *Builder) buildIndirection(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	expr, err := b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))
	if err != nil {
		return nil, err
	}

	index, err := b.buildScalar(ctx, scalar.Child(1).(opt.ScalarExpr))
	if err != nil {
		return nil, err
	}

	return tree.NewTypedIndirectionExpr(expr, index, scalar.DataType()), nil
}

func (b *Builder) buildCollate(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	expr, err := b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))
	if err != nil {
		return nil, err
	}

	return tree.NewTypedCollateExpr(expr, scalar.(*memo.CollateExpr).Locale), nil
}

func (b *Builder) buildArrayFlatten(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	af := scalar.(*memo.ArrayFlattenExpr)

	// The subquery here should always be uncorrelated: if it were not, we would
	// have converted it to an aggregation.
	if !af.Input.Relational().OuterCols.Empty() {
		panic(errors.AssertionFailedf("input to ArrayFlatten should be uncorrelated"))
	}

	root, err := b.buildRelational(af.Input)
	if err != nil {
		return nil, err
	}

	typ := b.mem.Metadata().ColumnMeta(af.RequestedCol).Type
	e := b.addSubquery(
		exec.SubqueryAllRows, typ, root.root, af.OriginalExpr,
		int64(af.Input.Relational().Statistics().RowCountIfAvailable()),
	)

	return tree.NewTypedArrayFlattenExpr(e), nil
}

func (b *Builder) buildIfErr(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	ifErr := scalar.(*memo.IfErrExpr)
	cond, err := b.buildScalar(ctx, ifErr.Cond)
	if err != nil {
		return nil, err
	}

	var orElse tree.TypedExpr
	if ifErr.OrElse.ChildCount() > 0 {
		orElse, err = b.buildScalar(ctx, ifErr.OrElse.Child(0).(opt.ScalarExpr))
		if err != nil {
			return nil, err
		}
	}

	var errCode tree.TypedExpr
	if ifErr.ErrCode.ChildCount() > 0 {
		errCode, err = b.buildScalar(ctx, ifErr.ErrCode.Child(0).(opt.ScalarExpr))
		if err != nil {
			return nil, err
		}
	}

	return tree.NewTypedIfErrExpr(cond, orElse, errCode), nil
}

func (b *Builder) buildItem(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	return b.buildScalar(ctx, scalar.Child(0).(opt.ScalarExpr))
}

func (b *Builder) buildAny(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	any := scalar.(*memo.AnyExpr)
	// We cannot execute correlated subqueries.
	if !any.Input.Relational().OuterCols.Empty() {
		return nil, b.decorrelationError()
	}

	// Build the execution plan for the input subquery.
	plan, err := b.buildRelational(any.Input)
	if err != nil {
		return nil, err
	}

	// Construct tuple type of columns in the row.
	contents := make([]*types.T, plan.numOutputCols())
	plan.outputCols.ForEach(func(key, val int) {
		contents[val] = b.mem.Metadata().ColumnMeta(opt.ColumnID(key)).Type
	})
	typs := types.MakeTuple(contents)
	subqueryExpr := b.addSubquery(
		exec.SubqueryAnyRows, typs, plan.root, any.OriginalExpr,
		int64(any.Input.Relational().Statistics().RowCountIfAvailable()),
	)

	// Build the scalar value that is compared against each row.
	scalarExpr, err := b.buildScalar(ctx, any.Scalar)
	if err != nil {
		return nil, err
	}

	cmp := opt.ComparisonOpReverseMap[any.Cmp]
	return tree.NewTypedComparisonExprWithSubOp(
		treecmp.MakeComparisonOperator(treecmp.Any),
		treecmp.MakeComparisonOperator(cmp),
		scalarExpr,
		subqueryExpr,
	), nil
}

func (b *Builder) buildExistsSubquery(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	exists := scalar.(*memo.ExistsExpr)
	// We cannot execute correlated subqueries.
	if !exists.Input.Relational().OuterCols.Empty() {
		return nil, b.decorrelationError()
	}

	// Build the execution plan for the subquery. Note that the subquery could
	// have subqueries of its own which are added to b.subqueries.
	plan, err := b.buildRelational(exists.Input)
	if err != nil {
		return nil, err
	}

	return b.addSubquery(
		exec.SubqueryExists, types.Bool, plan.root, exists.OriginalExpr,
		int64(exists.Input.Relational().Statistics().RowCountIfAvailable()),
	), nil
}

func (b *Builder) buildSubquery(
	ctx *buildScalarCtx, scalar opt.ScalarExpr,
) (tree.TypedExpr, error) {
	subquery := scalar.(*memo.SubqueryExpr)
	input := subquery.Input

	// TODO(radu): for now we only support the trivial projection.
	cols := input.Relational().OutputCols
	if cols.Len() != 1 {
		return nil, errors.Errorf("subquery input with multiple columns")
	}

	// We cannot execute correlated subqueries.
	// TODO(mgartner): We can execute correlated subqueries by making them
	// routines, like we do below.
	if !input.Relational().OuterCols.Empty() {
		return nil, b.decorrelationError()
	}

	if b.planLazySubqueries {
		// Build lazily-evaluated subqueries as routines.
		//
		// Note: We reuse the optimizer and memo from the original expression
		// because we don't need to optimize the subquery input any further.
		// It's already been fully optimized because it is uncorrelated and has
		// no outer columns.
		//
		// TODO(mgartner): Uncorrelated subqueries only need to be evaluated
		// once. We should cache their result to avoid all this overhead for
		// every invocation.
		inputRowCount := int64(input.Relational().Statistics().RowCountIfAvailable())
		planFn := func(
			ctx context.Context, ref tree.RoutineExecFactory, stmtIdx int, args tree.Datums,
		) (tree.RoutinePlan, error) {
			ef := ref.(exec.Factory)
			eb := New(ctx, ef, b.optimizer, b.mem, b.catalog, input, b.evalCtx, false /* allowAutoCommit */, b.IsANSIDML)
			eb.disableTelemetry = true
			eb.planLazySubqueries = true
			plan, err := eb.buildRelational(input)
			if err != nil {
				return nil, err
			}
			if len(eb.subqueries) > 0 {
				return nil, expectedLazyRoutineError("subquery")
			}
			if len(eb.cascades) > 0 {
				return nil, expectedLazyRoutineError("cascade")
			}
			if len(eb.checks) > 0 {
				return nil, expectedLazyRoutineError("check")
			}
			return b.factory.ConstructPlan(
				plan.root, nil /* subqueries */, nil /* cascades */, nil /* checks */, inputRowCount,
			)
		}
		return tree.NewTypedRoutineExpr(
			"subquery",
			nil, /* args */
			planFn,
			1, /* numStmts */
			subquery.Typ,
			false, /* enableStepping */
			true,  /* calledOnNullInput */
		), nil
	}

	// Build the execution plan for the subquery. Note that the subquery could
	// have subqueries of its own which are added to b.subqueries.
	plan, err := b.buildRelational(input)
	if err != nil {
		return nil, err
	}

	// Build a subquery that is eagerly evaluated before the main query.
	return b.addSubquery(
		exec.SubqueryOneRow, subquery.Typ, plan.root, subquery.OriginalExpr,
		int64(input.Relational().Statistics().RowCountIfAvailable()),
	), nil
}

// addSubquery adds an entry to b.subqueries and creates a tree.Subquery
// expression node associated with it.
func (b *Builder) addSubquery(
	mode exec.SubqueryMode, typ *types.T, root exec.Node, originalExpr *tree.Subquery, rowCount int64,
) *tree.Subquery {
	var originalSelect tree.SelectStatement
	if originalExpr != nil {
		originalSelect = originalExpr.Select
	}
	exprNode := &tree.Subquery{
		Select: originalSelect,
		Exists: mode == exec.SubqueryExists,
	}
	exprNode.SetType(typ)
	b.subqueries = append(b.subqueries, exec.Subquery{
		ExprNode: exprNode,
		Mode:     mode,
		Root:     root,
		RowCount: rowCount,
	})
	// Associate the tree.Subquery expression node with this subquery
	// by index (1-based).
	exprNode.Idx = len(b.subqueries)
	return exprNode
}

// buildUDF builds a UDF expression into a typed expression that can be
// evaluated.
func (b *Builder) buildUDF(ctx *buildScalarCtx, scalar opt.ScalarExpr) (tree.TypedExpr, error) {
	udf := scalar.(*memo.UDFExpr)

	// Build the argument expressions.
	var err error
	var args tree.TypedExprs
	if len(udf.Args) > 0 {
		args = make(tree.TypedExprs, len(udf.Args))
		for i := range udf.Args {
			args[i], err = b.buildScalar(ctx, udf.Args[i])
			if err != nil {
				return nil, err
			}
		}
	}

	// argOrd returns the ordinal of the argument within the arguments list that
	// can be substituted for each reference to the given function parameter
	// column. If the given column does not represent a function parameter,
	// ok=false is returned.
	argOrd := func(col opt.ColumnID) (ord int, ok bool) {
		for i, param := range udf.Params {
			if col == param {
				return i, true
			}
		}
		return 0, false
	}

	// Create a tree.RoutinePlanFn that can plan the statements in the UDF body.
	// We do this planning in a separate memo. We use an exec.Factory passed to
	// the closure rather than b.factory to support executing plans that are
	// generated with explain.Factory.
	//
	// Note: the ref argument has type tree.RoutineExecFactory rather than
	// exec.Factory to avoid import cycles.
	//
	// Note: we put o outside of the function so we allocate it only once.
	var o xform.Optimizer
	planFn := func(
		ctx context.Context, ref tree.RoutineExecFactory, stmtIdx int, args tree.Datums,
	) (tree.RoutinePlan, error) {
		o.Init(ctx, b.evalCtx, b.catalog)
		f := o.Factory()
		stmt := udf.Body[stmtIdx]

		// Copy the expression into a new memo. Replace parameter references
		// with argument datums.
		var replaceFn norm.ReplaceFunc
		replaceFn = func(e opt.Expr) opt.Expr {
			if v, ok := e.(*memo.VariableExpr); ok {
				if ord, ok := argOrd(v.Col); ok {
					return f.ConstructConstVal(args[ord], v.Typ)
				}
			}
			return f.CopyAndReplaceDefault(e, replaceFn)
		}
		f.CopyAndReplace(stmt, stmt.PhysProps, replaceFn)

		// Optimize the memo.
		newRightSide, err := o.Optimize()
		if err != nil {
			return nil, err
		}

		// Build the memo into a plan.
		// TODO(mgartner): Add support for WITH expressions inside UDF bodies.
		// TODO(mgartner): Add support for subqueries inside UDF bodies.
		ef := ref.(exec.Factory)
		eb := New(ctx, ef, &o, f.Memo(), b.catalog, newRightSide, b.evalCtx, false /* allowAutoCommit */, b.IsANSIDML)
		eb.disableTelemetry = true
		eb.planLazySubqueries = true
		plan, err := eb.Build()
		if err != nil {
			if errors.IsAssertionFailure(err) {
				// Enhance the error with the EXPLAIN (OPT, VERBOSE) of the
				// inner expression.
				fmtFlags := memo.ExprFmtHideQualifications | memo.ExprFmtHideScalars |
					memo.ExprFmtHideTypes
				explainOpt := o.FormatExpr(newRightSide, fmtFlags)
				err = errors.WithDetailf(err, "routineExpr:\n%s", explainOpt)
			}
			return nil, err
		}
		return plan, nil
	}
	// Enable stepping for volatile functions so that statements within the UDF
	// see mutations made by the invoking statement and by previous executed
	// statements.
	enableStepping := udf.Volatility == volatility.Volatile
	return tree.NewTypedRoutineExpr(
		udf.Name,
		args,
		planFn,
		len(udf.Body),
		udf.Typ,
		enableStepping,
		udf.CalledOnNullInput,
	), nil
}

func expectedLazyRoutineError(typ string) error {
	return errors.AssertionFailedf("expected %s to be lazily planned as routines", typ)
}
