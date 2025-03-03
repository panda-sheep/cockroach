// Copyright 2016 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package sql

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/cockroach/pkg/docs"
	"github.com/cockroachdb/cockroach/pkg/kv"
	"github.com/cockroachdb/cockroach/pkg/security/username"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog/catenumpb"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog/descpb"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog/nstree"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog/schemadesc"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog/schemaexpr"
	"github.com/cockroachdb/cockroach/pkg/sql/catalog/tabledesc"
	"github.com/cockroachdb/cockroach/pkg/sql/privilege"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/builtins"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/builtins/builtinsregistry"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/catconstants"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/catid"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/semenumpb"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/sqlutil"
	"github.com/cockroachdb/cockroach/pkg/sql/types"
	"github.com/cockroachdb/cockroach/pkg/sql/vtable"
	"github.com/cockroachdb/cockroach/pkg/util/iterutil"
	"github.com/cockroachdb/cockroach/pkg/util/timeutil/pgdate"
	"github.com/cockroachdb/errors"
	"github.com/lib/pq/oid"
	"golang.org/x/text/collate"
)

const (
	pgCatalogName = catconstants.PgCatalogName
)

var pgCatalogNameDString = tree.NewDString(pgCatalogName)

// informationSchema lists all the table definitions for
// information_schema.
var informationSchema = virtualSchema{
	name: catconstants.InformationSchemaName,
	undefinedTables: buildStringSet(
		// Generated with:
		// select distinct '"'||table_name||'",' from information_schema.tables
		//    where table_schema='information_schema' order by table_name;
		"_pg_foreign_data_wrappers",
		"_pg_foreign_servers",
		"_pg_foreign_table_columns",
		"_pg_foreign_tables",
		"_pg_user_mappings",
		"sql_languages",
		"sql_packages",
		"sql_sizing_profiles",
	),
	tableDefs: map[descpb.ID]virtualSchemaDef{
		catconstants.InformationSchemaAdministrableRoleAuthorizationsID:   informationSchemaAdministrableRoleAuthorizations,
		catconstants.InformationSchemaApplicableRolesID:                   informationSchemaApplicableRoles,
		catconstants.InformationSchemaAttributesTableID:                   informationSchemaAttributesTable,
		catconstants.InformationSchemaCharacterSets:                       informationSchemaCharacterSets,
		catconstants.InformationSchemaCheckConstraintRoutineUsageTableID:  informationSchemaCheckConstraintRoutineUsageTable,
		catconstants.InformationSchemaCheckConstraints:                    informationSchemaCheckConstraints,
		catconstants.InformationSchemaCollationCharacterSetApplicability:  informationSchemaCollationCharacterSetApplicability,
		catconstants.InformationSchemaCollations:                          informationSchemaCollations,
		catconstants.InformationSchemaColumnColumnUsageTableID:            informationSchemaColumnColumnUsageTable,
		catconstants.InformationSchemaColumnDomainUsageTableID:            informationSchemaColumnDomainUsageTable,
		catconstants.InformationSchemaColumnOptionsTableID:                informationSchemaColumnOptionsTable,
		catconstants.InformationSchemaColumnPrivilegesID:                  informationSchemaColumnPrivileges,
		catconstants.InformationSchemaColumnStatisticsTableID:             informationSchemaColumnStatisticsTable,
		catconstants.InformationSchemaColumnUDTUsageID:                    informationSchemaColumnUDTUsage,
		catconstants.InformationSchemaColumnsExtensionsTableID:            informationSchemaColumnsExtensionsTable,
		catconstants.InformationSchemaColumnsTableID:                      informationSchemaColumnsTable,
		catconstants.InformationSchemaConstraintColumnUsageTableID:        informationSchemaConstraintColumnUsageTable,
		catconstants.InformationSchemaConstraintTableUsageTableID:         informationSchemaConstraintTableUsageTable,
		catconstants.InformationSchemaDataTypePrivilegesTableID:           informationSchemaDataTypePrivilegesTable,
		catconstants.InformationSchemaDomainConstraintsTableID:            informationSchemaDomainConstraintsTable,
		catconstants.InformationSchemaDomainUdtUsageTableID:               informationSchemaDomainUdtUsageTable,
		catconstants.InformationSchemaDomainsTableID:                      informationSchemaDomainsTable,
		catconstants.InformationSchemaElementTypesTableID:                 informationSchemaElementTypesTable,
		catconstants.InformationSchemaEnabledRolesID:                      informationSchemaEnabledRoles,
		catconstants.InformationSchemaEnginesTableID:                      informationSchemaEnginesTable,
		catconstants.InformationSchemaEventsTableID:                       informationSchemaEventsTable,
		catconstants.InformationSchemaFilesTableID:                        informationSchemaFilesTable,
		catconstants.InformationSchemaForeignDataWrapperOptionsTableID:    informationSchemaForeignDataWrapperOptionsTable,
		catconstants.InformationSchemaForeignDataWrappersTableID:          informationSchemaForeignDataWrappersTable,
		catconstants.InformationSchemaForeignServerOptionsTableID:         informationSchemaForeignServerOptionsTable,
		catconstants.InformationSchemaForeignServersTableID:               informationSchemaForeignServersTable,
		catconstants.InformationSchemaForeignTableOptionsTableID:          informationSchemaForeignTableOptionsTable,
		catconstants.InformationSchemaForeignTablesTableID:                informationSchemaForeignTablesTable,
		catconstants.InformationSchemaInformationSchemaCatalogNameTableID: informationSchemaInformationSchemaCatalogNameTable,
		catconstants.InformationSchemaKeyColumnUsageTableID:               informationSchemaKeyColumnUsageTable,
		catconstants.InformationSchemaKeywordsTableID:                     informationSchemaKeywordsTable,
		catconstants.InformationSchemaOptimizerTraceTableID:               informationSchemaOptimizerTraceTable,
		catconstants.InformationSchemaParametersTableID:                   informationSchemaParametersTable,
		catconstants.InformationSchemaPartitionsTableID:                   informationSchemaPartitionsTable,
		catconstants.InformationSchemaPluginsTableID:                      informationSchemaPluginsTable,
		catconstants.InformationSchemaProcesslistTableID:                  informationSchemaProcesslistTable,
		catconstants.InformationSchemaProfilingTableID:                    informationSchemaProfilingTable,
		catconstants.InformationSchemaReferentialConstraintsTableID:       informationSchemaReferentialConstraintsTable,
		catconstants.InformationSchemaResourceGroupsTableID:               informationSchemaResourceGroupsTable,
		catconstants.InformationSchemaRoleColumnGrantsTableID:             informationSchemaRoleColumnGrantsTable,
		catconstants.InformationSchemaRoleRoutineGrantsTableID:            informationSchemaRoleRoutineGrantsTable,
		catconstants.InformationSchemaRoleTableGrantsID:                   informationSchemaRoleTableGrants,
		catconstants.InformationSchemaRoleUdtGrantsTableID:                informationSchemaRoleUdtGrantsTable,
		catconstants.InformationSchemaRoleUsageGrantsTableID:              informationSchemaRoleUsageGrantsTable,
		catconstants.InformationSchemaRoutinePrivilegesTableID:            informationSchemaRoutinePrivilegesTable,
		catconstants.InformationSchemaRoutineTableID:                      informationSchemaRoutineTable,
		catconstants.InformationSchemaSQLFeaturesTableID:                  informationSchemaSQLFeaturesTable,
		catconstants.InformationSchemaSQLImplementationInfoTableID:        informationSchemaSQLImplementationInfoTable,
		catconstants.InformationSchemaSQLPartsTableID:                     informationSchemaSQLPartsTable,
		catconstants.InformationSchemaSQLSizingTableID:                    informationSchemaSQLSizingTable,
		catconstants.InformationSchemaSchemataExtensionsTableID:           informationSchemaSchemataExtensionsTable,
		catconstants.InformationSchemaSchemataTableID:                     informationSchemaSchemataTable,
		catconstants.InformationSchemaSchemataTablePrivilegesID:           informationSchemaSchemataTablePrivileges,
		catconstants.InformationSchemaSequencesID:                         informationSchemaSequences,
		catconstants.InformationSchemaSessionVariables:                    informationSchemaSessionVariables,
		catconstants.InformationSchemaStGeometryColumnsTableID:            informationSchemaStGeometryColumnsTable,
		catconstants.InformationSchemaStSpatialReferenceSystemsTableID:    informationSchemaStSpatialReferenceSystemsTable,
		catconstants.InformationSchemaStUnitsOfMeasureTableID:             informationSchemaStUnitsOfMeasureTable,
		catconstants.InformationSchemaStatisticsTableID:                   informationSchemaStatisticsTable,
		catconstants.InformationSchemaTableConstraintTableID:              informationSchemaTableConstraintTable,
		catconstants.InformationSchemaTableConstraintsExtensionsTableID:   informationSchemaTableConstraintsExtensionsTable,
		catconstants.InformationSchemaTablePrivilegesID:                   informationSchemaTablePrivileges,
		catconstants.InformationSchemaTablesExtensionsTableID:             informationSchemaTablesExtensionsTable,
		catconstants.InformationSchemaTablesTableID:                       informationSchemaTablesTable,
		catconstants.InformationSchemaTablespacesExtensionsTableID:        informationSchemaTablespacesExtensionsTable,
		catconstants.InformationSchemaTablespacesTableID:                  informationSchemaTablespacesTable,
		catconstants.InformationSchemaTransformsTableID:                   informationSchemaTransformsTable,
		catconstants.InformationSchemaTriggeredUpdateColumnsTableID:       informationSchemaTriggeredUpdateColumnsTable,
		catconstants.InformationSchemaTriggersTableID:                     informationSchemaTriggersTable,
		catconstants.InformationSchemaTypePrivilegesID:                    informationSchemaTypePrivilegesTable,
		catconstants.InformationSchemaUdtPrivilegesTableID:                informationSchemaUdtPrivilegesTable,
		catconstants.InformationSchemaUsagePrivilegesTableID:              informationSchemaUsagePrivilegesTable,
		catconstants.InformationSchemaUserAttributesTableID:               informationSchemaUserAttributesTable,
		catconstants.InformationSchemaUserDefinedTypesTableID:             informationSchemaUserDefinedTypesTable,
		catconstants.InformationSchemaUserMappingOptionsTableID:           informationSchemaUserMappingOptionsTable,
		catconstants.InformationSchemaUserMappingsTableID:                 informationSchemaUserMappingsTable,
		catconstants.InformationSchemaUserPrivilegesID:                    informationSchemaUserPrivileges,
		catconstants.InformationSchemaViewColumnUsageTableID:              informationSchemaViewColumnUsageTable,
		catconstants.InformationSchemaViewRoutineUsageTableID:             informationSchemaViewRoutineUsageTable,
		catconstants.InformationSchemaViewTableUsageTableID:               informationSchemaViewTableUsageTable,
		catconstants.InformationSchemaViewsTableID:                        informationSchemaViewsTable,
	},
	tableValidator:             validateInformationSchemaTable,
	validWithNoDatabaseContext: true,
}

func buildStringSet(ss ...string) map[string]struct{} {
	m := map[string]struct{}{}
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

var (
	emptyString = tree.NewDString("")
	// information_schema was defined before the BOOLEAN data type was added to
	// the SQL specification. Because of this, boolean values are represented as
	// STRINGs. The BOOLEAN data type should NEVER be used in information_schema
	// tables. Instead, define columns as STRINGs and map bools to STRINGs using
	// yesOrNoDatum.
	yesString    = tree.NewDString("YES")
	noString     = tree.NewDString("NO")
	alwaysString = tree.NewDString("ALWAYS")
	neverString  = tree.NewDString("NEVER")
)

func yesOrNoDatum(b bool) tree.Datum {
	if b {
		return yesString
	}
	return noString
}

func alwaysOrNeverDatum(b bool) tree.Datum {
	if b {
		return alwaysString
	}
	return neverString
}

func dNameOrNull(s string) tree.Datum {
	if s == "" {
		return tree.DNull
	}
	return tree.NewDName(s)
}

func dIntFnOrNull(fn func() (int32, bool)) tree.Datum {
	if n, ok := fn(); ok {
		return tree.NewDInt(tree.DInt(n))
	}
	return tree.DNull
}

func validateInformationSchemaTable(table *descpb.TableDescriptor) error {
	// Make sure no tables have boolean columns.
	for i := range table.Columns {
		if table.Columns[i].Type.Family() == types.BoolFamily {
			return errors.Errorf("information_schema tables should never use BOOL columns. "+
				"See the comment about yesOrNoDatum. Found BOOL column in %s.", table.Name)
		}
	}
	return nil
}

var informationSchemaAdministrableRoleAuthorizations = virtualSchemaTable{
	comment: `roles for which the current user has admin option
` + docs.URL("information-schema.html#administrable_role_authorizations") + `
https://www.postgresql.org/docs/9.5/infoschema-administrable-role-authorizations.html`,
	schema: vtable.InformationSchemaAdministrableRoleAuthorizations,
	populate: func(
		ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error,
	) error {
		return populateRoleHierarchy(ctx, p, addRow, true /* onlyIsAdmin */)
	},
}

var informationSchemaApplicableRoles = virtualSchemaTable{
	comment: `roles available to the current user
` + docs.URL("information-schema.html#applicable_roles") + `
https://www.postgresql.org/docs/9.5/infoschema-applicable-roles.html`,
	schema: vtable.InformationSchemaApplicableRoles,
	populate: func(
		ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error,
	) error {
		return populateRoleHierarchy(ctx, p, addRow, false /* onlyIsAdmin */)
	},
}

func populateRoleHierarchy(
	ctx context.Context, p *planner, addRow func(...tree.Datum) error, onlyIsAdmin bool,
) error {
	allRoles, err := p.MemberOfWithAdminOption(ctx, p.User())
	if err != nil {
		return err
	}
	return forEachRoleMembership(
		ctx, p.ExecCfg().InternalExecutor, p.Txn(),
		func(role, member username.SQLUsername, isAdmin bool) error {
			// The ADMIN OPTION is inherited through the role hierarchy, and grantee
			// is supposed to be the role that has the ADMIN OPTION. The current user
			// inherits all the ADMIN OPTIONs of its ancestors.
			isRole := member == p.User()
			_, hasRole := allRoles[member]
			if (hasRole || isRole) && (!onlyIsAdmin || isAdmin) {
				if err := addRow(
					tree.NewDString(member.Normalized()), // grantee
					tree.NewDString(role.Normalized()),   // role_name
					yesOrNoDatum(isAdmin),                // is_grantable
				); err != nil {
					return err
				}
			}
			return nil
		},
	)
}

var informationSchemaCharacterSets = virtualSchemaTable{
	comment: `character sets available in the current database
` + docs.URL("information-schema.html#character_sets") + `
https://www.postgresql.org/docs/9.5/infoschema-character-sets.html`,
	schema: vtable.InformationSchemaCharacterSets,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachDatabaseDesc(ctx, p, nil /* all databases */, true, /* requiresPrivileges */
			func(db catalog.DatabaseDescriptor) error {
				return addRow(
					tree.DNull,                    // character_set_catalog
					tree.DNull,                    // character_set_schema
					tree.NewDString("UTF8"),       // character_set_name: UTF8 is the only available encoding
					tree.NewDString("UCS"),        // character_repertoire: UCS for UTF8 encoding
					tree.NewDString("UTF8"),       // form_of_use: same as the database encoding
					tree.NewDString(db.GetName()), // default_collate_catalog
					tree.DNull,                    // default_collate_schema
					tree.DNull,                    // default_collate_name
				)
			})
	},
}

var informationSchemaCheckConstraints = virtualSchemaTable{
	comment: `check constraints
` + docs.URL("information-schema.html#check_constraints") + `
https://www.postgresql.org/docs/9.5/infoschema-check-constraints.html`,
	schema: vtable.InformationSchemaCheckConstraints,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachTableDescWithTableLookup(ctx, p, dbContext, hideVirtual /* no constraints in virtual tables */, func(
			db catalog.DatabaseDescriptor,
			sc catalog.SchemaDescriptor,
			table catalog.TableDescriptor,
			tableLookup tableLookupFn,
		) error {
			dbNameStr := tree.NewDString(db.GetName())
			scNameStr := tree.NewDString(sc.GetName())
			for _, ck := range table.EnforcedCheckConstraints() {
				// Like with pg_catalog.pg_constraint, Postgres wraps the check
				// constraint expression in two pairs of parentheses.
				chkExprStr := tree.NewDString(fmt.Sprintf("((%s))", ck.GetExpr()))
				if err := addRow(
					dbNameStr,                     // constraint_catalog
					scNameStr,                     // constraint_schema
					tree.NewDString(ck.GetName()), // constraint_name
					chkExprStr,                    // check_clause
				); err != nil {
					return err
				}
			}

			// Unlike with pg_catalog.pg_constraint, Postgres also includes NOT
			// NULL column constraints in information_schema.check_constraints.
			// Cockroach doesn't track these constraints as check constraints,
			// but we can pull them off of the table's column descriptors.
			for _, column := range table.PublicColumns() {
				// Only visible, non-nullable columns are included.
				if column.IsHidden() || column.IsNullable() {
					continue
				}
				// Generate a unique name for each NOT NULL constraint. Postgres
				// uses the format <namespace_oid>_<table_oid>_<col_idx>_not_null.
				// We might as well do the same.
				conNameStr := tree.NewDString(fmt.Sprintf(
					"%s_%s_%d_not_null",
					schemaOid(sc.GetID()),
					tableOid(table.GetID()), column.Ordinal()+1,
				))
				chkExprStr := tree.NewDString(fmt.Sprintf(
					"%s IS NOT NULL", column.GetName(),
				))
				if err := addRow(
					dbNameStr,  // constraint_catalog
					scNameStr,  // constraint_schema
					conNameStr, // constraint_name
					chkExprStr, // check_clause
				); err != nil {
					return err
				}
			}
			return nil
		})
	},
}

var informationSchemaColumnPrivileges = virtualSchemaTable{
	comment: `column privilege grants (incomplete)
` + docs.URL("information-schema.html#column_privileges") + `
https://www.postgresql.org/docs/9.5/infoschema-column-privileges.html`,
	schema: vtable.InformationSchemaColumnPrivileges,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachTableDesc(ctx, p, dbContext, virtualMany, func(
			db catalog.DatabaseDescriptor, sc catalog.SchemaDescriptor, table catalog.TableDescriptor,
		) error {
			dbNameStr := tree.NewDString(db.GetName())
			scNameStr := tree.NewDString(sc.GetName())
			columndata := privilege.List{privilege.SELECT, privilege.INSERT, privilege.UPDATE} // privileges for column level granularity
			privDesc, err := p.getPrivilegeDescriptor(ctx, table)
			if err != nil {
				return err
			}
			for _, u := range privDesc.Users {
				for _, priv := range columndata {
					if priv.Mask()&u.Privileges != 0 {
						for _, cd := range table.PublicColumns() {
							if err := addRow(
								tree.DNull,                             // grantor
								tree.NewDString(u.User().Normalized()), // grantee
								dbNameStr,                              // table_catalog
								scNameStr,                              // table_schema
								tree.NewDString(table.GetName()),       // table_name
								tree.NewDString(cd.GetName()),          // column_name
								tree.NewDString(priv.String()),         // privilege_type
								tree.DNull,                             // is_grantable
							); err != nil {
								return err
							}
						}
					}
				}
			}
			return nil
		})
	},
}

var informationSchemaColumnsTable = virtualSchemaTable{
	comment: `table and view columns (incomplete)
` + docs.URL("information-schema.html#columns") + `
https://www.postgresql.org/docs/9.5/infoschema-columns.html`,
	schema: vtable.InformationSchemaColumns,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		// Get the collations for all comments of current database.
		comments, err := getComments(ctx, p)
		if err != nil {
			return err
		}
		// Push all comments of columns into map.
		commentMap := make(map[tree.DInt]map[tree.DInt]string)
		for _, comment := range comments {
			objID := tree.MustBeDInt(comment[0])
			objSubID := tree.MustBeDInt(comment[1])
			description := comment[2].String()
			commentType := tree.MustBeDInt(comment[3])
			if commentType == 2 {
				if commentMap[objID] == nil {
					commentMap[objID] = make(map[tree.DInt]string)
				}
				commentMap[objID][objSubID] = description
			}
		}

		return forEachTableDesc(ctx, p, dbContext, virtualMany, func(
			db catalog.DatabaseDescriptor, sc catalog.SchemaDescriptor, table catalog.TableDescriptor,
		) error {
			dbNameStr := tree.NewDString(db.GetName())
			scNameStr := tree.NewDString(sc.GetName())
			for _, column := range table.AccessibleColumns() {
				collationCatalog := tree.DNull
				collationSchema := tree.DNull
				collationName := tree.DNull
				if locale := column.GetType().Locale(); locale != "" {
					collationCatalog = dbNameStr
					collationSchema = pgCatalogNameDString
					collationName = tree.NewDString(locale)
				}
				colDefault := tree.DNull
				if column.HasDefault() {
					colExpr, err := schemaexpr.FormatExprForDisplay(
						ctx, table, column.GetDefaultExpr(), &p.semaCtx, p.SessionData(), tree.FmtParsable,
					)
					if err != nil {
						return err
					}
					colDefault = tree.NewDString(colExpr)
				}
				colComputed := emptyString
				if column.IsComputed() {
					colExpr, err := schemaexpr.FormatExprForDisplay(
						ctx, table, column.GetComputeExpr(), &p.semaCtx, p.SessionData(), tree.FmtSimple,
					)
					if err != nil {
						return err
					}
					colComputed = tree.NewDString(colExpr)
				}
				colGeneratedAsIdentity := emptyString
				if column.IsGeneratedAsIdentity() {
					if column.IsGeneratedAlwaysAsIdentity() {
						colGeneratedAsIdentity = tree.NewDString("ALWAYS")
					} else if column.IsGeneratedByDefaultAsIdentity() {
						colGeneratedAsIdentity = tree.NewDString("BY DEFAULT")
					} else {
						return errors.AssertionFailedf(
							"column %s is of wrong generated as identity type (neither ALWAYS nor BY DEFAULT)",
							column.GetName(),
						)
					}
				}

				// Match the comment belonging to current column from map,using table id and column id
				tableID := tree.DInt(table.GetID())
				columnID := tree.DInt(column.GetID())
				description := commentMap[tableID][columnID]

				// udt_schema is set to pg_catalog for builtin types. If, however, the
				// type is a user defined type, then we should fill this value based on
				// the schema it is under.
				udtSchema := pgCatalogNameDString
				typeMetaName := column.GetType().TypeMeta.Name
				if typeMetaName != nil {
					udtSchema = tree.NewDString(typeMetaName.Schema)
				}

				// Get the sequence option if it's an identity column.
				identityStart := tree.DNull
				identityIncrement := tree.DNull
				identityMax := tree.DNull
				identityMin := tree.DNull
				generatedAsIdentitySeqOpt, err := column.GetGeneratedAsIdentitySequenceOption(column.GetType().Width())
				if err != nil {
					return err
				}
				if generatedAsIdentitySeqOpt != nil {
					identityStart = tree.NewDString(strconv.FormatInt(generatedAsIdentitySeqOpt.Start, 10))
					identityIncrement = tree.NewDString(strconv.FormatInt(generatedAsIdentitySeqOpt.Increment, 10))
					identityMax = tree.NewDString(strconv.FormatInt(generatedAsIdentitySeqOpt.MaxValue, 10))
					identityMin = tree.NewDString(strconv.FormatInt(generatedAsIdentitySeqOpt.MinValue, 10))
				}

				err = addRow(
					dbNameStr,                         // table_catalog
					scNameStr,                         // table_schema
					tree.NewDString(table.GetName()),  // table_name
					tree.NewDString(column.GetName()), // column_name
					tree.NewDString(description),      // column_comment
					tree.NewDInt(tree.DInt(column.GetPGAttributeNum())), // ordinal_position
					colDefault,                        // column_default
					yesOrNoDatum(column.IsNullable()), // is_nullable
					tree.NewDString(column.GetType().InformationSchemaName()), // data_type
					characterMaximumLength(column.GetType()),                  // character_maximum_length
					characterOctetLength(column.GetType()),                    // character_octet_length
					numericPrecision(column.GetType()),                        // numeric_precision
					numericPrecisionRadix(column.GetType()),                   // numeric_precision_radix
					numericScale(column.GetType()),                            // numeric_scale
					datetimePrecision(column.GetType()),                       // datetime_precision
					tree.DNull,                                                // interval_type
					tree.DNull,                                                // interval_precision
					tree.DNull,                                                // character_set_catalog
					tree.DNull,                                                // character_set_schema
					tree.DNull,                                                // character_set_name
					collationCatalog,                                          // collation_catalog
					collationSchema,                                           // collation_schema
					collationName,                                             // collation_name
					tree.DNull,                                                // domain_catalog
					tree.DNull,                                                // domain_schema
					tree.DNull,                                                // domain_name
					dbNameStr,                                                 // udt_catalog
					udtSchema,                                                 // udt_schema
					tree.NewDString(column.GetType().PGName()), // udt_name
					tree.DNull, // scope_catalog
					tree.DNull, // scope_schema
					tree.DNull, // scope_name
					tree.DNull, // maximum_cardinality
					tree.DNull, // dtd_identifier
					tree.DNull, // is_self_referencing
					yesOrNoDatum(column.IsGeneratedAsIdentity()), // is_identity
					colGeneratedAsIdentity,                       // identity_generation
					identityStart,                                // identity_start
					identityIncrement,                            // identity_increment
					identityMax,                                  // identity_maximum
					identityMin,                                  // identity_minimum
					// TODO(janexing): we don't support CYCLE syntax for sequences yet.
					// https://github.com/cockroachdb/cockroach/issues/20961
					tree.DNull,                              // identity_cycle
					alwaysOrNeverDatum(column.IsComputed()), // is_generated
					colComputed,                             // generation_expression
					yesOrNoDatum(table.IsTable() &&
						!table.IsVirtualTable() &&
						!column.IsComputed(),
					), // is_updatable
					yesOrNoDatum(column.IsHidden()),               // is_hidden
					tree.NewDString(column.GetType().SQLString()), // crdb_sql_type
				)
				if err != nil {
					return err
				}
			}
			return nil
		})
	},
}

var informationSchemaColumnUDTUsage = virtualSchemaTable{
	comment: `columns with user defined types
` + docs.URL("information-schema.html#column_udt_usage") + `
https://www.postgresql.org/docs/current/infoschema-column-udt-usage.html`,
	schema: vtable.InformationSchemaColumnUDTUsage,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachTableDesc(ctx, p, dbContext, hideVirtual,
			func(db catalog.DatabaseDescriptor, sc catalog.SchemaDescriptor, table catalog.TableDescriptor) error {
				dbNameStr := tree.NewDString(db.GetName())
				scNameStr := tree.NewDString(sc.GetName())
				tbNameStr := tree.NewDString(table.GetName())
				for _, col := range table.PublicColumns() {
					if !col.GetType().UserDefined() {
						continue
					}
					if err := addRow(
						tree.NewDString(col.GetType().TypeMeta.Name.Catalog), // UDT_CATALOG
						tree.NewDString(col.GetType().TypeMeta.Name.Schema),  // UDT_SCHEMA
						tree.NewDString(col.GetType().TypeMeta.Name.Name),    // UDT_NAME
						dbNameStr,                      // TABLE_CATALOG
						scNameStr,                      // TABLE_SCHEMA
						tbNameStr,                      // TABLE_NAME
						tree.NewDString(col.GetName()), // COLUMN_NAME
					); err != nil {
						return err
					}
				}
				return nil
			},
		)
	},
}

var informationSchemaEnabledRoles = virtualSchemaTable{
	comment: `roles for the current user
` + docs.URL("information-schema.html#enabled_roles") + `
https://www.postgresql.org/docs/9.5/infoschema-enabled-roles.html`,
	schema: vtable.InformationSchemaEnabledRoles,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		currentUser := p.SessionData().User()
		memberMap, err := p.MemberOfWithAdminOption(ctx, currentUser)
		if err != nil {
			return err
		}

		// The current user is always listed.
		if err := addRow(
			tree.NewDString(currentUser.Normalized()), // role_name: the current user
		); err != nil {
			return err
		}

		for roleName := range memberMap {
			if err := addRow(
				tree.NewDString(roleName.Normalized()), // role_name
			); err != nil {
				return err
			}
		}

		return nil
	},
}

// characterMaximumLength returns the declared maximum length of
// characters if the type is a character or bit string data
// type. Returns false if the data type is not a character or bit
// string, or if the string's length is not bounded.
func characterMaximumLength(colType *types.T) tree.Datum {
	return dIntFnOrNull(func() (int32, bool) {
		// "char" columns have a width of 1, but should report a NULL maximum
		// character length.
		if colType.Oid() == oid.T_char {
			return 0, false
		}
		switch colType.Family() {
		case types.StringFamily, types.CollatedStringFamily, types.BitFamily:
			if colType.Width() > 0 {
				return colType.Width(), true
			}
		}
		return 0, false
	})
}

// characterOctetLength returns the maximum possible length in
// octets of a datum if the T is a character string. Returns
// false if the data type is not a character string, or if the
// string's length is not bounded.
func characterOctetLength(colType *types.T) tree.Datum {
	return dIntFnOrNull(func() (int32, bool) {
		// "char" columns have a width of 1, but should report a NULL octet
		// length.
		if colType.Oid() == oid.T_char {
			return 0, false
		}
		switch colType.Family() {
		case types.StringFamily, types.CollatedStringFamily:
			if colType.Width() > 0 {
				return colType.Width() * utf8.UTFMax, true
			}
		}
		return 0, false
	})
}

// numericPrecision returns the declared or implicit precision of numeric
// data types. Returns false if the data type is not numeric, or if the precision
// of the numeric type is not bounded.
func numericPrecision(colType *types.T) tree.Datum {
	return dIntFnOrNull(func() (int32, bool) {
		switch colType.Family() {
		case types.IntFamily:
			return colType.Width(), true
		case types.FloatFamily:
			if colType.Width() == 32 {
				return 24, true
			}
			return 53, true
		case types.DecimalFamily:
			if colType.Precision() > 0 {
				return colType.Precision(), true
			}
		}
		return 0, false
	})
}

// numericPrecisionRadix returns the implicit precision radix of
// numeric data types. Returns false if the data type is not numeric.
func numericPrecisionRadix(colType *types.T) tree.Datum {
	return dIntFnOrNull(func() (int32, bool) {
		switch colType.Family() {
		case types.IntFamily:
			return 2, true
		case types.FloatFamily:
			return 2, true
		case types.DecimalFamily:
			return 10, true
		}
		return 0, false
	})
}

// NumericScale returns the declared or implicit precision of exact numeric
// data types. Returns false if the data type is not an exact numeric, or if the
// scale of the exact numeric type is not bounded.
func numericScale(colType *types.T) tree.Datum {
	return dIntFnOrNull(func() (int32, bool) {
		switch colType.Family() {
		case types.IntFamily:
			return 0, true
		case types.DecimalFamily:
			if colType.Precision() > 0 {
				return colType.Width(), true
			}
		}
		return 0, false
	})
}

// datetimePrecision returns the declared or implicit precision of Time,
// Timestamp or Interval data types. Returns false if the data type is not
// a Time, Timestamp or Interval.
func datetimePrecision(colType *types.T) tree.Datum {
	return dIntFnOrNull(func() (int32, bool) {
		switch colType.Family() {
		case types.TimeFamily, types.TimeTZFamily, types.TimestampFamily, types.TimestampTZFamily, types.IntervalFamily:
			return colType.Precision(), true
		}
		return 0, false
	})
}

var informationSchemaConstraintColumnUsageTable = virtualSchemaTable{
	comment: `columns usage by constraints
https://www.postgresql.org/docs/9.5/infoschema-constraint-column-usage.html`,
	schema: vtable.InformationSchemaConstraintColumnUsage,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachTableDescWithTableLookup(ctx, p, dbContext, hideVirtual /* no constraints in virtual tables */, func(
			db catalog.DatabaseDescriptor,
			sc catalog.SchemaDescriptor,
			table catalog.TableDescriptor,
			tableLookup tableLookupFn,
		) error {
			dbNameStr := tree.NewDString(db.GetName())
			scNameStr := tree.NewDString(sc.GetName())
			for _, c := range table.AllConstraints() {
				conNameStr := tree.NewDString(c.GetName())
				refSchema := sc
				refTable := table
				var cols []catalog.Column
				if ck := c.AsCheck(); ck != nil {
					cols = table.CheckConstraintColumns(ck)
				} else if fk := c.AsForeignKey(); fk != nil {
					var err error
					refTable, err = tableLookup.getTableByID(fk.GetReferencedTableID())
					if err != nil {
						return errors.NewAssertionErrorWithWrappedErrf(err,
							"error resolving table %d referenced in foreign key %q in table %q",
							fk.GetReferencedTableID(), fk.GetName(), table.GetName())
					}
					refSchema, err = tableLookup.getSchemaByID(refTable.GetParentSchemaID())
					if err != nil {
						return errors.NewAssertionErrorWithWrappedErrf(err,
							"error resolving schema %d referenced in foreign key %q in table %q",
							refTable.GetParentSchemaID(), fk.GetName(), table.GetName())
					}
					cols = refTable.ForeignKeyReferencedColumns(fk)
				} else if uwi := c.AsUniqueWithIndex(); uwi != nil {
					cols = table.IndexKeyColumns(uwi)
				} else if uwoi := c.AsUniqueWithoutIndex(); uwoi != nil {
					cols = table.UniqueWithoutIndexColumns(uwoi)
				}
				for _, col := range cols {
					if err := addRow(
						dbNameStr,                            // table_catalog
						tree.NewDString(refSchema.GetName()), // table_schema
						tree.NewDString(refTable.GetName()),  // table_name
						tree.NewDString(col.GetName()),       // column_name
						dbNameStr,                            // constraint_catalog
						scNameStr,                            // constraint_schema
						conNameStr,                           // constraint_name
					); err != nil {
						return err
					}
				}
			}
			return nil
		})
	},
}

// MySQL:    https://dev.mysql.com/doc/refman/5.7/en/key-column-usage-table.html
var informationSchemaKeyColumnUsageTable = virtualSchemaTable{
	comment: `column usage by indexes and key constraints
` + docs.URL("information-schema.html#key_column_usage") + `
https://www.postgresql.org/docs/9.5/infoschema-key-column-usage.html`,
	schema: vtable.InformationSchemaKeyColumnUsage,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachTableDescWithTableLookup(ctx, p, dbContext, hideVirtual /* no constraints in virtual tables */, func(
			db catalog.DatabaseDescriptor,
			sc catalog.SchemaDescriptor,
			table catalog.TableDescriptor,
			tableLookup tableLookupFn,
		) error {
			dbNameStr := tree.NewDString(db.GetName())
			scNameStr := tree.NewDString(sc.GetName())
			tbNameStr := tree.NewDString(table.GetName())
			for _, c := range table.AllConstraints() {
				cstNameStr := tree.NewDString(c.GetName())
				var cols []catalog.Column
				// Only Primary Key, Foreign Key, and Unique constraints are included.
				if fk := c.AsForeignKey(); fk != nil {
					cols = table.ForeignKeyOriginColumns(fk)
				} else if uwi := c.AsUniqueWithIndex(); uwi != nil {
					cols = table.IndexKeyColumns(uwi)
				} else if uwoi := c.AsUniqueWithoutIndex(); uwoi != nil {
					cols = table.UniqueWithoutIndexColumns(uwoi)
				}
				for pos, col := range cols {
					ordinalPos := tree.NewDInt(tree.DInt(pos + 1))
					uniquePos := tree.DNull
					if c.AsForeignKey() != nil {
						uniquePos = ordinalPos
					}
					if err := addRow(
						dbNameStr,                      // constraint_catalog
						scNameStr,                      // constraint_schema
						cstNameStr,                     // constraint_name
						dbNameStr,                      // table_catalog
						scNameStr,                      // table_schema
						tbNameStr,                      // table_name
						tree.NewDString(col.GetName()), // column_name
						ordinalPos,                     // ordinal_position, 1-indexed
						uniquePos,                      // position_in_unique_constraint
					); err != nil {
						return err
					}
				}
			}
			return nil
		})
	},
}

// Postgres: https://www.postgresql.org/docs/9.6/static/infoschema-parameters.html
// MySQL:    https://dev.mysql.com/doc/refman/5.7/en/parameters-table.html
var informationSchemaParametersTable = virtualSchemaTable{
	comment: `built-in function parameters (empty - introspection not yet supported)
https://www.postgresql.org/docs/9.5/infoschema-parameters.html`,
	schema: vtable.InformationSchemaParameters,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var (
	matchOptionFull    = tree.NewDString("FULL")
	matchOptionPartial = tree.NewDString("PARTIAL")
	matchOptionNone    = tree.NewDString("NONE")

	matchOptionMap = map[semenumpb.Match]tree.Datum{
		semenumpb.Match_SIMPLE:  matchOptionNone,
		semenumpb.Match_FULL:    matchOptionFull,
		semenumpb.Match_PARTIAL: matchOptionPartial,
	}

	refConstraintRuleNoAction   = tree.NewDString("NO ACTION")
	refConstraintRuleRestrict   = tree.NewDString("RESTRICT")
	refConstraintRuleSetNull    = tree.NewDString("SET NULL")
	refConstraintRuleSetDefault = tree.NewDString("SET DEFAULT")
	refConstraintRuleCascade    = tree.NewDString("CASCADE")
)

func dStringForFKAction(action semenumpb.ForeignKeyAction) tree.Datum {
	switch action {
	case semenumpb.ForeignKeyAction_NO_ACTION:
		return refConstraintRuleNoAction
	case semenumpb.ForeignKeyAction_RESTRICT:
		return refConstraintRuleRestrict
	case semenumpb.ForeignKeyAction_SET_NULL:
		return refConstraintRuleSetNull
	case semenumpb.ForeignKeyAction_SET_DEFAULT:
		return refConstraintRuleSetDefault
	case semenumpb.ForeignKeyAction_CASCADE:
		return refConstraintRuleCascade
	}
	panic(errors.Errorf("unexpected ForeignKeyReference_Action: %v", action))
}

// MySQL:    https://dev.mysql.com/doc/refman/5.7/en/referential-constraints-table.html
var informationSchemaReferentialConstraintsTable = virtualSchemaTable{
	comment: `foreign key constraints
` + docs.URL("information-schema.html#referential_constraints") + `
https://www.postgresql.org/docs/9.5/infoschema-referential-constraints.html`,
	schema: vtable.InformationSchemaReferentialConstraints,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachTableDescWithTableLookup(ctx, p, dbContext, hideVirtual /* no constraints in virtual tables */, func(
			db catalog.DatabaseDescriptor,
			sc catalog.SchemaDescriptor,
			table catalog.TableDescriptor,
			tableLookup tableLookupFn,
		) error {
			dbNameStr := tree.NewDString(db.GetName())
			scNameStr := tree.NewDString(sc.GetName())
			tbNameStr := tree.NewDString(table.GetName())
			for _, fk := range table.OutboundForeignKeys() {
				refTable, err := tableLookup.getTableByID(fk.GetReferencedTableID())
				if err != nil {
					return err
				}
				var matchType = tree.DNull
				if r, ok := matchOptionMap[fk.Match()]; ok {
					matchType = r
				}
				refConstraint, err := tabledesc.FindFKReferencedUniqueConstraint(refTable, fk)
				if err != nil {
					return err
				}
				if err := addRow(
					dbNameStr,                                // constraint_catalog
					scNameStr,                                // constraint_schema
					tree.NewDString(fk.GetName()),            // constraint_name
					dbNameStr,                                // unique_constraint_catalog
					scNameStr,                                // unique_constraint_schema
					tree.NewDString(refConstraint.GetName()), // unique_constraint_name
					matchType,                                // match_option
					dStringForFKAction(fk.OnUpdate()),        // update_rule
					dStringForFKAction(fk.OnDelete()),        // delete_rule
					tbNameStr,                                // table_name
					tree.NewDString(refTable.GetName()),      // referenced_table_name
				); err != nil {
					return err
				}
			}
			return nil
		})
	},
}

// Postgres: https://www.postgresql.org/docs/9.6/static/infoschema-role-table-grants.html
// MySQL:    missing
var informationSchemaRoleTableGrants = virtualSchemaTable{
	comment: `privileges granted on table or views (incomplete; see also information_schema.table_privileges; may contain excess users or roles)
` + docs.URL("information-schema.html#role_table_grants") + `
https://www.postgresql.org/docs/9.5/infoschema-role-table-grants.html`,
	schema: vtable.InformationSchemaRoleTableGrants,
	// This is the same as information_schema.table_privileges. In postgres, this virtual table does
	// not show tables with grants provided through PUBLIC, but table_privileges does.
	// Since we don't have the PUBLIC concept, the two virtual tables are identical.
	populate: populateTablePrivileges,
}

// MySQL:    https://dev.mysql.com/doc/mysql-infoschema-excerpt/5.7/en/routines-table.html
var informationSchemaRoutineTable = virtualSchemaTable{
	comment: `built-in functions (empty - introspection not yet supported)
https://www.postgresql.org/docs/9.5/infoschema-routines.html`,
	schema: vtable.InformationSchemaRoutines,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

// MySQL:    https://dev.mysql.com/doc/refman/5.7/en/schemata-table.html
var informationSchemaSchemataTable = virtualSchemaTable{
	comment: `database schemas (may contain schemata without permission)
` + docs.URL("information-schema.html#schemata") + `
https://www.postgresql.org/docs/9.5/infoschema-schemata.html`,
	schema: vtable.InformationSchemaSchemata,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachDatabaseDesc(ctx, p, dbContext, true, /* requiresPrivileges */
			func(db catalog.DatabaseDescriptor) error {
				return forEachSchema(ctx, p, db, true /* requiresPrivileges */, func(sc catalog.SchemaDescriptor) error {
					return addRow(
						tree.NewDString(db.GetName()), // catalog_name
						tree.NewDString(sc.GetName()), // schema_name
						tree.DNull,                    // default_character_set_name
						tree.DNull,                    // sql_path
						yesOrNoDatum(sc.SchemaKind() == catalog.SchemaUserDefined), // crdb_is_user_defined
					)
				})
			})
	},
}

var builtinTypePrivileges = []struct {
	grantee *tree.DString
	kind    *tree.DString
}{
	{tree.NewDString(username.RootUser), tree.NewDString(privilege.ALL.String())},
	{tree.NewDString(username.AdminRole), tree.NewDString(privilege.ALL.String())},
	{tree.NewDString(username.PublicRole), tree.NewDString(privilege.USAGE.String())},
}

// Custom; PostgreSQL has data_type_privileges, which only shows one row per type,
// which may result in confusing semantics for the user compared to this table
// which has one row for each grantee.
var informationSchemaTypePrivilegesTable = virtualSchemaTable{
	comment: `type privileges (incomplete; may contain excess users or roles)
` + docs.URL("information-schema.html#type_privileges"),
	schema: vtable.InformationSchemaTypePrivileges,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachDatabaseDesc(ctx, p, dbContext, true, /* requiresPrivileges */
			func(db catalog.DatabaseDescriptor) error {
				dbNameStr := tree.NewDString(db.GetName())
				pgCatalogStr := tree.NewDString("pg_catalog")
				// Generate one for each existing type.
				for _, typ := range types.OidToType {
					typeNameStr := tree.NewDString(typ.Name())
					for _, it := range builtinTypePrivileges {
						if err := addRow(
							it.grantee,   // grantee
							dbNameStr,    // type_catalog
							pgCatalogStr, // type_schema
							typeNameStr,  // type_name
							it.kind,      // privilege_type
							noString,     // is_grantable
						); err != nil {
							return err
						}
					}
				}

				// And for all user defined types.
				return forEachTypeDesc(ctx, p, db, func(db catalog.DatabaseDescriptor, sc catalog.SchemaDescriptor, typeDesc catalog.TypeDescriptor) error {
					scNameStr := tree.NewDString(sc.GetName())
					typeNameStr := tree.NewDString(typeDesc.GetName())
					// TODO(knz): This should filter for the current user, see
					// https://github.com/cockroachdb/cockroach/issues/35572
					privs := typeDesc.GetPrivileges().Show(privilege.Type, true /* showImplicitOwnerPrivs */)
					for _, u := range privs {
						userNameStr := tree.NewDString(u.User.Normalized())
						for _, priv := range u.Privileges {
							// We use this function to check for the grant option so that the
							// object owner also gets is_grantable=true.
							isGrantable, err := p.CheckGrantOptionsForUser(
								ctx, typeDesc.GetPrivileges(), typeDesc, []privilege.Kind{priv.Kind}, u.User,
							)
							if err != nil {
								return err
							}
							if err := addRow(
								userNameStr,                         // grantee
								dbNameStr,                           // type_catalog
								scNameStr,                           // type_schema
								typeNameStr,                         // type_name
								tree.NewDString(priv.Kind.String()), // privilege_type
								yesOrNoDatum(isGrantable),           // is_grantable
							); err != nil {
								return err
							}
						}
					}
					return nil
				})
			})
	},
}

// MySQL:    https://dev.mysql.com/doc/refman/5.7/en/schema-privileges-table.html
var informationSchemaSchemataTablePrivileges = virtualSchemaTable{
	comment: `schema privileges (incomplete; may contain excess users or roles)
` + docs.URL("information-schema.html#schema_privileges"),
	schema: vtable.InformationSchemaSchemaPrivileges,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachDatabaseDesc(ctx, p, dbContext, true, /* requiresPrivileges */
			func(db catalog.DatabaseDescriptor) error {
				return forEachSchema(ctx, p, db, true /* requiresPrivileges */, func(sc catalog.SchemaDescriptor) error {
					privs := sc.GetPrivileges().Show(privilege.Schema, true /* showImplicitOwnerPrivs */)
					dbNameStr := tree.NewDString(db.GetName())
					scNameStr := tree.NewDString(sc.GetName())
					// TODO(knz): This should filter for the current user, see
					// https://github.com/cockroachdb/cockroach/issues/35572
					for _, u := range privs {
						userNameStr := tree.NewDString(u.User.Normalized())
						for _, priv := range u.Privileges {
							// We use this function to check for the grant option so that the
							// object owner also gets is_grantable=true.
							isGrantable, err := p.CheckGrantOptionsForUser(
								ctx, sc.GetPrivileges(), sc, []privilege.Kind{priv.Kind}, u.User,
							)
							if err != nil {
								return err
							}
							if err := addRow(
								userNameStr,                         // grantee
								dbNameStr,                           // table_catalog
								scNameStr,                           // table_schema
								tree.NewDString(priv.Kind.String()), // privilege_type
								yesOrNoDatum(isGrantable),           // is_grantable
							); err != nil {
								return err
							}
						}
					}
					return nil
				})
			})
	},
}

var (
	indexDirectionNA   = tree.NewDString("N/A")
	indexDirectionAsc  = tree.NewDString(catenumpb.IndexColumn_ASC.String())
	indexDirectionDesc = tree.NewDString(catenumpb.IndexColumn_DESC.String())
)

func dStringForIndexDirection(dir catenumpb.IndexColumn_Direction) tree.Datum {
	switch dir {
	case catenumpb.IndexColumn_ASC:
		return indexDirectionAsc
	case catenumpb.IndexColumn_DESC:
		return indexDirectionDesc
	}
	panic("unreachable")
}

var informationSchemaSequences = virtualSchemaTable{
	comment: `sequences
` + docs.URL("information-schema.html#sequences") + `
https://www.postgresql.org/docs/9.5/infoschema-sequences.html`,
	schema: vtable.InformationSchemaSequences,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachTableDesc(ctx, p, dbContext, hideVirtual, /* no sequences in virtual schemas */
			func(db catalog.DatabaseDescriptor, sc catalog.SchemaDescriptor, table catalog.TableDescriptor) error {
				if !table.IsSequence() {
					return nil
				}
				return addRow(
					tree.NewDString(db.GetName()),    // catalog
					tree.NewDString(sc.GetName()),    // schema
					tree.NewDString(table.GetName()), // name
					tree.NewDString("bigint"),        // type
					tree.NewDInt(64),                 // numeric precision
					tree.NewDInt(2),                  // numeric precision radix
					tree.NewDInt(0),                  // numeric scale
					tree.NewDString(strconv.FormatInt(table.GetSequenceOpts().Start, 10)),     // start value
					tree.NewDString(strconv.FormatInt(table.GetSequenceOpts().MinValue, 10)),  // min value
					tree.NewDString(strconv.FormatInt(table.GetSequenceOpts().MaxValue, 10)),  // max value
					tree.NewDString(strconv.FormatInt(table.GetSequenceOpts().Increment, 10)), // increment
					noString, // cycle
				)
			})
	},
}

// Postgres: missing
// MySQL:    https://dev.mysql.com/doc/refman/5.7/en/statistics-table.html
var informationSchemaStatisticsTable = virtualSchemaTable{
	comment: `index metadata and statistics (incomplete)
` + docs.URL("information-schema.html#statistics"),
	schema: vtable.InformationSchemaStatistics,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachTableDesc(ctx, p, dbContext, hideVirtual, /* virtual tables have no indexes */
			func(db catalog.DatabaseDescriptor, sc catalog.SchemaDescriptor, table catalog.TableDescriptor) error {
				dbNameStr := tree.NewDString(db.GetName())
				scNameStr := tree.NewDString(sc.GetName())
				tbNameStr := tree.NewDString(table.GetName())

				appendRow := func(index catalog.Index, colName string, sequence int,
					direction tree.Datum, isStored, isImplicit bool,
				) error {
					return addRow(
						dbNameStr,                           // table_catalog
						scNameStr,                           // table_schema
						tbNameStr,                           // table_name
						yesOrNoDatum(!index.IsUnique()),     // non_unique
						scNameStr,                           // index_schema
						tree.NewDString(index.GetName()),    // index_name
						tree.NewDInt(tree.DInt(sequence)),   // seq_in_index
						tree.NewDString(colName),            // column_name
						tree.DNull,                          // collation
						tree.DNull,                          // cardinality
						direction,                           // direction
						yesOrNoDatum(isStored),              // storing
						yesOrNoDatum(isImplicit),            // implicit
						yesOrNoDatum(!index.IsNotVisible()), // is_visible
					)
				}

				return catalog.ForEachIndex(table, catalog.IndexOpts{}, func(index catalog.Index) error {
					// Columns in the primary key that aren't in index.KeyColumnNames or
					// index.StoreColumnNames are implicit columns in the index.
					var implicitCols map[string]struct{}
					var hasImplicitCols bool
					if index.HasOldStoredColumns() {
						// Old STORING format: implicit columns are extra columns minus stored
						// columns.
						hasImplicitCols = index.NumKeySuffixColumns() > index.NumSecondaryStoredColumns()
					} else {
						// New STORING format: implicit columns are extra columns.
						hasImplicitCols = index.NumKeySuffixColumns() > 0
					}
					if hasImplicitCols {
						implicitCols = make(map[string]struct{})
						for i := 0; i < table.GetPrimaryIndex().NumKeyColumns(); i++ {
							col := table.GetPrimaryIndex().GetKeyColumnName(i)
							implicitCols[col] = struct{}{}
						}
					}

					sequence := 1
					for i := 0; i < index.NumKeyColumns(); i++ {
						col := index.GetKeyColumnName(i)
						// We add a row for each column of index.
						dir := dStringForIndexDirection(index.GetKeyColumnDirection(i))
						if err := appendRow(
							index,
							col,
							sequence,
							dir,
							false,
							i < index.ExplicitColumnStartIdx(),
						); err != nil {
							return err
						}
						sequence++
						delete(implicitCols, col)
					}
					for i := 0; i < index.NumPrimaryStoredColumns()+index.NumSecondaryStoredColumns(); i++ {
						col := index.GetStoredColumnName(i)
						// We add a row for each stored column of index.
						if err := appendRow(index, col, sequence,
							indexDirectionNA, true, false); err != nil {
							return err
						}
						sequence++
						delete(implicitCols, col)
					}
					if len(implicitCols) > 0 {
						// In order to have the implicit columns reported in a
						// deterministic order, we will add all of them in the
						// same order as they are mentioned in the primary key.
						//
						// Note that simply iterating over implicitCols map
						// produces non-deterministic output.
						for i := 0; i < table.GetPrimaryIndex().NumKeyColumns(); i++ {
							col := table.GetPrimaryIndex().GetKeyColumnName(i)
							if _, isImplicit := implicitCols[col]; isImplicit {
								// We add a row for each implicit column of index.
								if err := appendRow(index, col, sequence,
									indexDirectionAsc, index.IsUnique(), true); err != nil {
									return err
								}
								sequence++
							}
						}
					}
					return nil
				})
			})
	},
}

// MySQL:    https://dev.mysql.com/doc/refman/5.7/en/table-constraints-table.html
var informationSchemaTableConstraintTable = virtualSchemaTable{
	comment: `table constraints
` + docs.URL("information-schema.html#table_constraints") + `
https://www.postgresql.org/docs/9.5/infoschema-table-constraints.html`,
	schema: vtable.InformationSchemaTableConstraint,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachTableDescWithTableLookup(ctx, p, dbContext, hideVirtual, /* virtual tables have no constraints */
			func(
				db catalog.DatabaseDescriptor,
				sc catalog.SchemaDescriptor,
				table catalog.TableDescriptor,
				tableLookup tableLookupFn,
			) error {
				dbNameStr := tree.NewDString(db.GetName())
				scNameStr := tree.NewDString(sc.GetName())
				tbNameStr := tree.NewDString(table.GetName())

				for _, c := range table.AllConstraints() {
					kind := catconstants.ConstraintTypeUnique
					if c.AsCheck() != nil {
						kind = catconstants.ConstraintTypeCheck
					} else if c.AsForeignKey() != nil {
						kind = catconstants.ConstraintTypeFK
					} else if u := c.AsUniqueWithIndex(); u != nil && u.Primary() {
						kind = catconstants.ConstraintTypePK
					}
					if err := addRow(
						dbNameStr,                     // constraint_catalog
						scNameStr,                     // constraint_schema
						tree.NewDString(c.GetName()),  // constraint_name
						dbNameStr,                     // table_catalog
						scNameStr,                     // table_schema
						tbNameStr,                     // table_name
						tree.NewDString(string(kind)), // constraint_type
						yesOrNoDatum(false),           // is_deferrable
						yesOrNoDatum(false),           // initially_deferred
					); err != nil {
						return err
					}
				}
				// Unlike with pg_catalog.pg_constraint, Postgres also includes NOT
				// NULL column constraints in information_schema.check_constraints.
				// Cockroach doesn't track these constraints as check constraints,
				// but we can pull them off of the table's column descriptors.
				for _, col := range table.PublicColumns() {
					if col.IsNullable() {
						continue
					}
					// NOT NULL column constraints are implemented as a CHECK in postgres.
					conNameStr := tree.NewDString(fmt.Sprintf(
						"%s_%s_%d_not_null",
						schemaOid(sc.GetID()),
						tableOid(table.GetID()), col.Ordinal()+1,
					))
					if err := addRow(
						dbNameStr,                // constraint_catalog
						scNameStr,                // constraint_schema
						conNameStr,               // constraint_name
						dbNameStr,                // table_catalog
						scNameStr,                // table_schema
						tbNameStr,                // table_name
						tree.NewDString("CHECK"), // constraint_type
						yesOrNoDatum(false),      // is_deferrable
						yesOrNoDatum(false),      // initially_deferred
					); err != nil {
						return err
					}
				}
				return nil
			})
	},
}

// Postgres: not provided
// MySQL:    https://dev.mysql.com/doc/refman/5.7/en/user-privileges-table.html
// TODO(knz): this introspection facility is of dubious utility.
var informationSchemaUserPrivileges = virtualSchemaTable{
	comment: `grantable privileges (incomplete)`,
	schema:  vtable.InformationSchemaUserPrivileges,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachDatabaseDesc(ctx, p, dbContext, true, /* requiresPrivileges */
			func(dbDesc catalog.DatabaseDescriptor) error {
				dbNameStr := tree.NewDString(dbDesc.GetName())
				for _, u := range []string{username.RootUser, username.AdminRole} {
					grantee := tree.NewDString(u)
					for _, p := range privilege.GetValidPrivilegesForObject(privilege.Table).SortedNames() {
						if err := addRow(
							grantee,            // grantee
							dbNameStr,          // table_catalog
							tree.NewDString(p), // privilege_type
							tree.DNull,         // is_grantable
						); err != nil {
							return err
						}
					}
				}
				return nil
			})
	},
}

// MySQL:    https://dev.mysql.com/doc/refman/5.7/en/table-privileges-table.html
var informationSchemaTablePrivileges = virtualSchemaTable{
	comment: `privileges granted on table or views (incomplete; may contain excess users or roles)
` + docs.URL("information-schema.html#table_privileges") + `
https://www.postgresql.org/docs/9.5/infoschema-table-privileges.html`,
	schema:   vtable.InformationSchemaTablePrivileges,
	populate: populateTablePrivileges,
}

// populateTablePrivileges is used to populate both table_privileges and role_table_grants.
func populateTablePrivileges(
	ctx context.Context,
	p *planner,
	dbContext catalog.DatabaseDescriptor,
	addRow func(...tree.Datum) error,
) error {
	return forEachTableDesc(ctx, p, dbContext, virtualMany,
		func(db catalog.DatabaseDescriptor, sc catalog.SchemaDescriptor, table catalog.TableDescriptor) error {
			dbNameStr := tree.NewDString(db.GetName())
			scNameStr := tree.NewDString(sc.GetName())
			tbNameStr := tree.NewDString(table.GetName())
			// TODO(knz): This should filter for the current user, see
			// https://github.com/cockroachdb/cockroach/issues/35572
			tableType := table.GetObjectType()
			desc, err := p.getPrivilegeDescriptor(ctx, table)
			if err != nil {
				return err
			}
			for _, u := range desc.Show(tableType, true /* showImplicitOwnerPrivs */) {
				granteeNameStr := tree.NewDString(u.User.Normalized())
				for _, priv := range u.Privileges {
					// We use this function to check for the grant option so that the
					// object owner also gets is_grantable=true.
					privs, err := p.getPrivilegeDescriptor(ctx, table)
					if err != nil {
						return err
					}
					isGrantable, err := p.CheckGrantOptionsForUser(
						ctx, privs, table, []privilege.Kind{priv.Kind}, u.User,
					)
					if err != nil {
						return err
					}
					if err := addRow(
						tree.DNull,                          // grantor
						granteeNameStr,                      // grantee
						dbNameStr,                           // table_catalog
						scNameStr,                           // table_schema
						tbNameStr,                           // table_name
						tree.NewDString(priv.Kind.String()), // privilege_type
						yesOrNoDatum(isGrantable),           // is_grantable
						yesOrNoDatum(priv.Kind == privilege.SELECT), // with_hierarchy
					); err != nil {
						return err
					}
				}
			}
			return nil
		})
}

var (
	tableTypeSystemView = tree.NewDString("SYSTEM VIEW")
	tableTypeBaseTable  = tree.NewDString("BASE TABLE")
	tableTypeView       = tree.NewDString("VIEW")
	tableTypeTemporary  = tree.NewDString("LOCAL TEMPORARY")
)

var informationSchemaTablesTable = virtualSchemaTable{
	comment: `tables and views
` + docs.URL("information-schema.html#tables") + `
https://www.postgresql.org/docs/9.5/infoschema-tables.html`,
	schema: vtable.InformationSchemaTables,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachTableDesc(ctx, p, dbContext, virtualMany, addTablesTableRow(addRow))
	},
}

func addTablesTableRow(
	addRow func(...tree.Datum) error,
) func(
	db catalog.DatabaseDescriptor,
	sc catalog.SchemaDescriptor,
	table catalog.TableDescriptor,
) error {
	return func(db catalog.DatabaseDescriptor, sc catalog.SchemaDescriptor, table catalog.TableDescriptor) error {
		if table.IsSequence() {
			return nil
		}
		tableType := tableTypeBaseTable
		insertable := yesString
		if table.IsVirtualTable() {
			tableType = tableTypeSystemView
			insertable = noString
		} else if table.IsView() {
			tableType = tableTypeView
			insertable = noString
		} else if table.IsTemporary() {
			tableType = tableTypeTemporary
		}
		dbNameStr := tree.NewDString(db.GetName())
		scNameStr := tree.NewDString(sc.GetName())
		tbNameStr := tree.NewDString(table.GetName())
		return addRow(
			dbNameStr,  // table_catalog
			scNameStr,  // table_schema
			tbNameStr,  // table_name
			tableType,  // table_type
			insertable, // is_insertable_into
			tree.NewDInt(tree.DInt(table.GetVersion())), // version
		)
	}
}

// Postgres: https://www.postgresql.org/docs/9.6/static/infoschema-views.html
// MySQL:    https://dev.mysql.com/doc/refman/5.7/en/views-table.html
var informationSchemaViewsTable = virtualSchemaTable{
	comment: `views (incomplete)
` + docs.URL("information-schema.html#views") + `
https://www.postgresql.org/docs/9.5/infoschema-views.html`,
	schema: vtable.InformationSchemaViews,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return forEachTableDesc(ctx, p, dbContext, hideVirtual, /* virtual schemas have no views */
			func(db catalog.DatabaseDescriptor, sc catalog.SchemaDescriptor, table catalog.TableDescriptor) error {
				if !table.IsView() {
					return nil
				}
				// Note that the view query printed will not include any column aliases
				// specified outside the initial view query into the definition returned,
				// unlike Postgres. For example, for the view created via
				//  `CREATE VIEW (a) AS SELECT b FROM foo`
				// we'll only print `SELECT b FROM foo` as the view definition here,
				// while Postgres would more accurately print `SELECT b AS a FROM foo`.
				// TODO(a-robinson): Insert column aliases into view query once we
				// have a semantic query representation to work with (#10083).
				return addRow(
					tree.NewDString(db.GetName()),         // table_catalog
					tree.NewDString(sc.GetName()),         // table_schema
					tree.NewDString(table.GetName()),      // table_name
					tree.NewDString(table.GetViewQuery()), // view_definition
					tree.DNull,                            // check_option
					noString,                              // is_updatable
					noString,                              // is_insertable_into
					noString,                              // is_trigger_updatable
					noString,                              // is_trigger_deletable
					noString,                              // is_trigger_insertable_into
				)
			})
	},
}

// Postgres: https://www.postgresql.org/docs/current/infoschema-collations.html
// MySQL:    https://dev.mysql.com/doc/refman/8.0/en/information-schema-collations-table.html
var informationSchemaCollations = virtualSchemaTable{
	comment: `shows the collations available in the current database
https://www.postgresql.org/docs/current/infoschema-collations.html`,
	schema: vtable.InformationSchemaCollations,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		dbNameStr := tree.NewDString(p.CurrentDatabase())
		add := func(collName string) error {
			return addRow(
				dbNameStr,
				pgCatalogNameDString,
				tree.NewDString(collName),
				// Always NO PAD (The alternative PAD SPACE is not supported.)
				tree.NewDString("NO PAD"),
			)
		}
		if err := add(tree.DefaultCollationTag); err != nil {
			return err
		}
		for _, tag := range collate.Supported() {
			collName := tag.String()
			if err := add(collName); err != nil {
				return err
			}
		}
		return nil
	},
}

// Postgres: https://www.postgresql.org/docs/current/infoschema-collation-character-set-applicab.html
// MySQL:    https://dev.mysql.com/doc/refman/8.0/en/information-schema-collation-character-set-applicability-table.html
var informationSchemaCollationCharacterSetApplicability = virtualSchemaTable{
	comment: `identifies which character set the available collations are 
applicable to. As UTF-8 is the only available encoding this table does not
provide much useful information.
https://www.postgresql.org/docs/current/infoschema-collation-character-set-applicab.html`,
	schema: vtable.InformationSchemaCollationCharacterSetApplicability,
	populate: func(ctx context.Context, p *planner, dbContext catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		dbNameStr := tree.NewDString(p.CurrentDatabase())
		add := func(collName string) error {
			return addRow(
				dbNameStr,                 // collation_catalog
				pgCatalogNameDString,      // collation_schema
				tree.NewDString(collName), // collation_name
				tree.DNull,                // character_set_catalog
				tree.DNull,                // character_set_schema
				tree.NewDString("UTF8"),   // character_set_name: UTF8 is the only available encoding
			)
		}
		if err := add(tree.DefaultCollationTag); err != nil {
			return err
		}
		for _, tag := range collate.Supported() {
			collName := tag.String()
			if err := add(collName); err != nil {
				return err
			}
		}
		return nil
	},
}

var informationSchemaSessionVariables = virtualSchemaTable{
	comment: `exposes the session variables.`,
	schema:  vtable.InformationSchemaSessionVariables,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		for _, vName := range varNames {
			gen := varGen[vName]
			value, err := gen.Get(&p.extendedEvalCtx, p.Txn())
			if err != nil {
				return err
			}
			if err := addRow(
				tree.NewDString(vName),
				tree.NewDString(value),
			); err != nil {
				return err
			}
		}
		return nil
	},
}

var informationSchemaRoutinePrivilegesTable = virtualSchemaTable{
	comment: "routine_privileges was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaRoutinePrivileges,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaRoleRoutineGrantsTable = virtualSchemaTable{
	comment: "privileges granted on functions (incomplete; only contains privileges of user-defined functions)",
	schema:  vtable.InformationSchemaRoleRoutineGrants,
	populate: func(ctx context.Context, p *planner, db catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		var dbDescs []catalog.DatabaseDescriptor
		if db == nil {
			var err error
			dbDescs, err = p.Descriptors().GetAllDatabaseDescriptors(ctx, p.Txn())
			if err != nil {
				return err
			}
		} else {
			dbDescs = append(dbDescs, db)
		}
		for _, db := range dbDescs {
			dbNameStr := tree.NewDString(db.GetName())
			exPriv := tree.NewDString(privilege.EXECUTE.String())
			roleNameForBuiltins := []*tree.DString{
				tree.NewDString(username.RootUser),
				tree.NewDString(username.PublicRole),
			}
			for _, name := range builtins.AllBuiltinNames() {
				parts := strings.Split(name, ".")
				if len(parts) > 2 || len(parts) == 0 {
					// This shouldn't happen in theory.
					return errors.AssertionFailedf("invalid builtin function name: %s", name)
				}

				var fnNameStr string
				var fnName *tree.DString
				var scNameStr *tree.DString
				if len(parts) == 2 {
					scNameStr = tree.NewDString(parts[0])
					fnNameStr = parts[1]
					fnName = tree.NewDString(fnNameStr)
				} else {
					scNameStr = tree.NewDString(catconstants.PgCatalogName)
					fnNameStr = name
					fnName = tree.NewDString(fnNameStr)
				}

				_, overloads := builtinsregistry.GetBuiltinProperties(name)
				for _, o := range overloads {
					fnSpecificName := tree.NewDString(fmt.Sprintf("%s_%d", fnNameStr, o.Oid))
					for _, grantee := range roleNameForBuiltins {
						if err := addRow(
							tree.DNull, // grantor
							grantee,
							dbNameStr,      // specific_catalog
							scNameStr,      // specific_schema
							fnSpecificName, // specific_name
							dbNameStr,      // routine_catalog
							scNameStr,      // routine_schema
							fnName,         // routine_name
							exPriv,         // privilege_type
							noString,       // is_grantable
						); err != nil {
							return err
						}
					}
				}
			}

			err := db.ForEachSchema(func(id descpb.ID, name string) error {
				sc, err := p.Descriptors().ByIDWithLeased(p.txn).WithoutNonPublic().Get().Schema(ctx, id)
				if err != nil {
					return err
				}
				return sc.ForEachFunctionOverload(func(overload descpb.SchemaDescriptor_FunctionOverload) error {
					fn, err := p.Descriptors().MutableByID(p.txn).Function(ctx, overload.ID)
					if err != nil {
						return err
					}
					privs := fn.GetPrivileges()
					scNameStr := tree.NewDString(sc.GetName())
					fnSpecificName := tree.NewDString(fmt.Sprintf("%s_%d", fn.GetName(), catid.FuncIDToOID(fn.GetID())))
					fnName := tree.NewDString(fn.GetName())
					// EXECUTE is the only privilege kind relevant to functions.
					if err := addRow(
						tree.DNull, // grantor
						tree.NewDString(privs.Owner().Normalized()), // grantee
						dbNameStr,      // specific_catalog
						scNameStr,      // specific_schema
						fnSpecificName, // specific_name
						dbNameStr,      // routine_catalog
						scNameStr,      // routine_schema
						fnName,         // routine_name
						exPriv,         // privilege_type
						yesString,      // is_grantable
					); err != nil {
						return err
					}
					for _, user := range privs.Users {
						if !privilege.EXECUTE.IsSetIn(user.Privileges) {
							continue
						}
						if err := addRow(
							tree.DNull, // grantor
							tree.NewDString(user.User().Normalized()), // grantee
							dbNameStr,      // specific_catalog
							scNameStr,      // specific_schema
							fnSpecificName, // specific_name
							dbNameStr,      // routine_catalog
							scNameStr,      // routine_schema
							fnName,         // routine_name
							exPriv,         // privilege_type
							yesOrNoDatum(privilege.EXECUTE.IsSetIn(user.WithGrantOption)), // is_grantable
						); err != nil {
							return err
						}
					}
					return nil
				})
			})
			if err != nil {
				return err
			}
		}
		return nil
	},
}

var informationSchemaElementTypesTable = virtualSchemaTable{
	comment: "element_types was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaElementTypes,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaRoleUdtGrantsTable = virtualSchemaTable{
	comment: "role_udt_grants was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaRoleUdtGrants,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaColumnOptionsTable = virtualSchemaTable{
	comment: "column_options was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaColumnOptions,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaForeignDataWrapperOptionsTable = virtualSchemaTable{
	comment: "foreign_data_wrapper_options was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaForeignDataWrapperOptions,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaTransformsTable = virtualSchemaTable{
	comment: "transforms was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaTransforms,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaViewColumnUsageTable = virtualSchemaTable{
	comment: "view_column_usage was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaViewColumnUsage,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaInformationSchemaCatalogNameTable = virtualSchemaTable{
	comment: "information_schema_catalog_name was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaInformationSchemaCatalogName,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaForeignTablesTable = virtualSchemaTable{
	comment: "foreign_tables was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaForeignTables,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaViewRoutineUsageTable = virtualSchemaTable{
	comment: "view_routine_usage was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaViewRoutineUsage,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaRoleColumnGrantsTable = virtualSchemaTable{
	comment: "role_column_grants was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaRoleColumnGrants,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaAttributesTable = virtualSchemaTable{
	comment: "attributes was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaAttributes,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaDomainConstraintsTable = virtualSchemaTable{
	comment: "domain_constraints was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaDomainConstraints,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaUserMappingsTable = virtualSchemaTable{
	comment: "user_mappings was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaUserMappings,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaCheckConstraintRoutineUsageTable = virtualSchemaTable{
	comment: "check_constraint_routine_usage was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaCheckConstraintRoutineUsage,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaColumnDomainUsageTable = virtualSchemaTable{
	comment: "column_domain_usage was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaColumnDomainUsage,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaForeignDataWrappersTable = virtualSchemaTable{
	comment: "foreign_data_wrappers was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaForeignDataWrappers,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaColumnColumnUsageTable = virtualSchemaTable{
	comment: "column_column_usage was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaColumnColumnUsage,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaSQLSizingTable = virtualSchemaTable{
	comment: "sql_sizing was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaSQLSizing,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaUsagePrivilegesTable = virtualSchemaTable{
	comment: "usage_privileges was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaUsagePrivileges,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaDomainsTable = virtualSchemaTable{
	comment: "domains was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaDomains,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaSQLImplementationInfoTable = virtualSchemaTable{
	comment: "sql_implementation_info was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaSQLImplementationInfo,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaUdtPrivilegesTable = virtualSchemaTable{
	comment: "udt_privileges was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaUdtPrivileges,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaPartitionsTable = virtualSchemaTable{
	comment: "partitions was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaPartitions,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaTablespacesExtensionsTable = virtualSchemaTable{
	comment: "tablespaces_extensions was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaTablespacesExtensions,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaResourceGroupsTable = virtualSchemaTable{
	comment: "resource_groups was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaResourceGroups,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaForeignServerOptionsTable = virtualSchemaTable{
	comment: "foreign_server_options was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaForeignServerOptions,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaStUnitsOfMeasureTable = virtualSchemaTable{
	comment: "st_units_of_measure was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaStUnitsOfMeasure,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaSchemataExtensionsTable = virtualSchemaTable{
	comment: "schemata_extensions was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaSchemataExtensions,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaColumnStatisticsTable = virtualSchemaTable{
	comment: "column_statistics was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaColumnStatistics,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaConstraintTableUsageTable = virtualSchemaTable{
	comment: "constraint_table_usage was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaConstraintTableUsage,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaDataTypePrivilegesTable = virtualSchemaTable{
	comment: "data_type_privileges was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaDataTypePrivileges,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaRoleUsageGrantsTable = virtualSchemaTable{
	comment: "role_usage_grants was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaRoleUsageGrants,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaFilesTable = virtualSchemaTable{
	comment: "files was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaFiles,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaEnginesTable = virtualSchemaTable{
	comment: "engines was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaEngines,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaForeignTableOptionsTable = virtualSchemaTable{
	comment: "foreign_table_options was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaForeignTableOptions,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaEventsTable = virtualSchemaTable{
	comment: "events was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaEvents,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaDomainUdtUsageTable = virtualSchemaTable{
	comment: "domain_udt_usage was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaDomainUdtUsage,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaUserAttributesTable = virtualSchemaTable{
	comment: "user_attributes was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaUserAttributes,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaKeywordsTable = virtualSchemaTable{
	comment: "keywords was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaKeywords,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaUserMappingOptionsTable = virtualSchemaTable{
	comment: "user_mapping_options was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaUserMappingOptions,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaOptimizerTraceTable = virtualSchemaTable{
	comment: "optimizer_trace was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaOptimizerTrace,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaTableConstraintsExtensionsTable = virtualSchemaTable{
	comment: "table_constraints_extensions was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaTableConstraintsExtensions,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaColumnsExtensionsTable = virtualSchemaTable{
	comment: "columns_extensions was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaColumnsExtensions,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaUserDefinedTypesTable = virtualSchemaTable{
	comment: "user_defined_types was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaUserDefinedTypes,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaSQLFeaturesTable = virtualSchemaTable{
	comment: "sql_features was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaSQLFeatures,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaStGeometryColumnsTable = virtualSchemaTable{
	comment: "st_geometry_columns was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaStGeometryColumns,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaSQLPartsTable = virtualSchemaTable{
	comment: "sql_parts was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaSQLParts,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaPluginsTable = virtualSchemaTable{
	comment: "plugins was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaPlugins,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaStSpatialReferenceSystemsTable = virtualSchemaTable{
	comment: "st_spatial_reference_systems was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaStSpatialReferenceSystems,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaProcesslistTable = virtualSchemaTable{
	comment: "processlist was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaProcesslist,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaForeignServersTable = virtualSchemaTable{
	comment: "foreign_servers was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaForeignServers,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaTriggeredUpdateColumnsTable = virtualSchemaTable{
	comment: "triggered_update_columns was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaTriggeredUpdateColumns,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaTriggersTable = virtualSchemaTable{
	comment: "triggers was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaTriggers,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaTablesExtensionsTable = virtualSchemaTable{
	comment: "tables_extensions was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaTablesExtensions,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaProfilingTable = virtualSchemaTable{
	comment: "profiling was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaProfiling,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaTablespacesTable = virtualSchemaTable{
	comment: "tablespaces was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaTablespaces,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

var informationSchemaViewTableUsageTable = virtualSchemaTable{
	comment: "view_table_usage was created for compatibility and is currently unimplemented",
	schema:  vtable.InformationSchemaViewTableUsage,
	populate: func(ctx context.Context, p *planner, _ catalog.DatabaseDescriptor, addRow func(...tree.Datum) error) error {
		return nil
	},
	unimplemented: true,
}

// forEachSchema iterates over the physical and virtual schemas.
func forEachSchema(
	ctx context.Context,
	p *planner,
	dbContext catalog.DatabaseDescriptor,
	requiresPrivileges bool,
	fn func(sc catalog.SchemaDescriptor) error,
) error {
	forEachDatabase := func(db catalog.DatabaseDescriptor) error {
		c, err := p.Descriptors().GetAllSchemasInDatabase(ctx, p.txn, db)
		if err != nil {
			return err
		}
		var schemas []catalog.SchemaDescriptor
		if err := c.ForEachDescriptor(func(desc catalog.Descriptor) error {
			if requiresPrivileges {
				canSeeDescriptor, err := userCanSeeDescriptor(ctx, p, desc, db, false /* allowAdding */)
				if err != nil {
					return err
				}
				if !canSeeDescriptor {
					return nil
				}
			}
			sc, err := catalog.AsSchemaDescriptor(desc)
			schemas = append(schemas, sc)
			return err
		}); err != nil {
			return err
		}
		sort.Slice(schemas, func(i int, j int) bool {
			return schemas[i].GetName() < schemas[j].GetName()
		})
		for _, sc := range schemas {
			if err := fn(sc); err != nil {
				return err
			}
		}
		return nil
	}

	if dbContext != nil {
		return iterutil.Map(forEachDatabase(dbContext))
	}
	c, err := p.Descriptors().GetAllDatabases(ctx, p.txn)
	if err != nil {
		return err
	}
	return c.ForEachDescriptor(func(desc catalog.Descriptor) error {
		db, err := catalog.AsDatabaseDescriptor(desc)
		if err != nil {
			return err
		}
		return forEachDatabase(db)
	})
}

// forEachDatabaseDesc calls a function for the given DatabaseDescriptor, or if
// it is nil, retrieves all database descriptors and iterates through them in
// lexicographical order with respect to their name. If privileges are required,
// the function is only called if the user has privileges on the database.
func forEachDatabaseDesc(
	ctx context.Context,
	p *planner,
	dbContext catalog.DatabaseDescriptor,
	requiresPrivileges bool,
	fn func(descriptor catalog.DatabaseDescriptor) error,
) error {
	var dbDescs []catalog.DatabaseDescriptor
	if dbContext == nil {
		allDbDescs, err := p.Descriptors().GetAllDatabaseDescriptors(ctx, p.txn)
		if err != nil {
			return err
		}
		dbDescs = allDbDescs
	} else {
		dbDescs = append(dbDescs, dbContext)
	}

	// Ignore databases that the user cannot see. We add a special case for the
	// current database. This is because we currently allow a user to connect
	// to a database even without the CONNECT privilege, but it would be poor
	// UX to not show the current database in pg_catalog/information_schema
	// tables.
	// See https://github.com/cockroachdb/cockroach/issues/59875.
	for _, dbDesc := range dbDescs {
		canSeeDescriptor := !requiresPrivileges
		if requiresPrivileges {
			hasPriv, err := userCanSeeDescriptor(ctx, p, dbDesc, nil /* parentDBDesc */, false /* allowAdding */)
			if err != nil {
				return err
			}
			canSeeDescriptor = hasPriv || p.CurrentDatabase() == dbDesc.GetName()
		}
		if canSeeDescriptor {
			if err := fn(dbDesc); err != nil {
				return err
			}
		}
	}

	return nil
}

// forEachTypeDesc calls a function for each TypeDescriptor. If dbContext is
// not nil, then the function is called for only TypeDescriptors within the
// given database.
func forEachTypeDesc(
	ctx context.Context,
	p *planner,
	dbContext catalog.DatabaseDescriptor,
	fn func(db catalog.DatabaseDescriptor, sc catalog.SchemaDescriptor, typ catalog.TypeDescriptor) error,
) (err error) {
	var all nstree.Catalog
	if dbContext != nil &&
		useIndexLookupForDescriptorsInDatabase.Get(&p.EvalContext().Settings.SV) {
		all, err = p.Descriptors().GetAllDescriptorsForDatabase(ctx, p.txn, dbContext)
	} else {
		all, err = p.Descriptors().GetAllDescriptors(ctx, p.txn)
	}
	if err != nil {
		return err
	}
	lCtx := newInternalLookupCtx(all.OrderedDescriptors(), dbContext)
	for _, id := range lCtx.typIDs {
		typ := lCtx.typDescs[id]
		dbDesc, err := lCtx.getDatabaseByID(typ.GetParentID())
		if err != nil {
			continue
		}
		sc, err := lCtx.getSchemaByID(typ.GetParentSchemaID())
		if err != nil {
			return err
		}
		canSeeDescriptor, err := userCanSeeDescriptor(ctx, p, typ, dbDesc, false /* allowAdding */)
		if err != nil {
			return err
		}
		if !canSeeDescriptor {
			continue
		}
		if err := fn(dbDesc, sc, typ); err != nil {
			return err
		}
	}
	return nil
}

// forEachTableDesc retrieves all table descriptors from the current
// database and all system databases and iterates through them. For
// each table, the function will call fn with its respective database
// and table descriptor.
//
// The dbContext argument specifies in which database context we are
// requesting the descriptors. In context nil all descriptors are
// visible, in non-empty contexts only the descriptors of that
// database are visible.
//
// The virtualOpts argument specifies how virtual tables are made
// visible.
func forEachTableDesc(
	ctx context.Context,
	p *planner,
	dbContext catalog.DatabaseDescriptor,
	virtualOpts virtualOpts,
	fn func(catalog.DatabaseDescriptor, catalog.SchemaDescriptor, catalog.TableDescriptor) error,
) error {
	return forEachTableDescWithTableLookup(ctx, p, dbContext, virtualOpts, func(
		db catalog.DatabaseDescriptor,
		sc catalog.SchemaDescriptor,
		table catalog.TableDescriptor,
		_ tableLookupFn,
	) error {
		return fn(db, sc, table)
	})
}

type virtualOpts int

const (
	// virtualMany iterates over virtual schemas in every catalog/database.
	virtualMany virtualOpts = iota
	// virtualCurrentDB iterates over virtual schemas in the current database.
	virtualCurrentDB
	// hideVirtual completely hides virtual schemas during iteration.
	hideVirtual
)

// forEachTableDescAll does the same as forEachTableDesc but also
// includes newly added non-public descriptors.
func forEachTableDescAll(
	ctx context.Context,
	p *planner,
	dbContext catalog.DatabaseDescriptor,
	virtualOpts virtualOpts,
	fn func(catalog.DatabaseDescriptor, catalog.SchemaDescriptor, catalog.TableDescriptor) error,
) error {
	return forEachTableDescAllWithTableLookup(ctx, p, dbContext, virtualOpts, func(
		db catalog.DatabaseDescriptor,
		sc catalog.SchemaDescriptor,
		table catalog.TableDescriptor,
		_ tableLookupFn,
	) error {
		return fn(db, sc, table)
	})
}

// forEachTableDescAllWithTableLookup is like forEachTableDescAll, but it also
// provides a tableLookupFn like forEachTableDescWithTableLookup. If validate is
// set to false descriptors will not be validated for existence or consistency
// hence fn should be able to handle nil-s.
func forEachTableDescAllWithTableLookup(
	ctx context.Context,
	p *planner,
	dbContext catalog.DatabaseDescriptor,
	virtualOpts virtualOpts,
	fn func(catalog.DatabaseDescriptor, catalog.SchemaDescriptor, catalog.TableDescriptor, tableLookupFn) error,
) error {
	return forEachTableDescWithTableLookupInternal(
		ctx, p, dbContext, virtualOpts, true /* allowAdding */, fn,
	)
}

// forEachTableDescWithTableLookup acts like forEachTableDesc, except it also provides a
// tableLookupFn when calling fn to allow callers to lookup fetched table descriptors
// on demand. This is important for callers dealing with objects like foreign keys, where
// the metadata for each object must be augmented by looking at the referenced table.
//
// The dbContext argument specifies in which database context we are
// requesting the descriptors.  In context "" all descriptors are
// visible, in non-empty contexts only the descriptors of that
// database are visible.
func forEachTableDescWithTableLookup(
	ctx context.Context,
	p *planner,
	dbContext catalog.DatabaseDescriptor,
	virtualOpts virtualOpts,
	fn func(catalog.DatabaseDescriptor, catalog.SchemaDescriptor, catalog.TableDescriptor, tableLookupFn) error,
) error {
	return forEachTableDescWithTableLookupInternal(
		ctx, p, dbContext, virtualOpts, false /* allowAdding */, fn,
	)
}

// forEachTableDescWithTableLookupInternal is the logic that supports
// forEachTableDescWithTableLookup.
//
// The allowAdding argument if true includes newly added tables that
// are not yet public.
// The validate argument if false turns off checking if the descriptor ids exist
// and if they are valid.
func forEachTableDescWithTableLookupInternal(
	ctx context.Context,
	p *planner,
	dbContext catalog.DatabaseDescriptor,
	virtualOpts virtualOpts,
	allowAdding bool,
	fn func(catalog.DatabaseDescriptor, catalog.SchemaDescriptor, catalog.TableDescriptor, tableLookupFn) error,
) (err error) {
	var all nstree.Catalog
	if dbContext != nil && useIndexLookupForDescriptorsInDatabase.Get(&p.EvalContext().Settings.SV) {
		all, err = p.Descriptors().GetAllDescriptorsForDatabase(ctx, p.txn, dbContext)
	} else {
		all, err = p.Descriptors().GetAllDescriptors(ctx, p.txn)
	}
	if err != nil {
		return err
	}
	return forEachTableDescWithTableLookupInternalFromDescriptors(
		ctx, p, dbContext, virtualOpts, allowAdding, all, fn)
}

func forEachTypeDescWithTableLookupInternalFromDescriptors(
	ctx context.Context,
	p *planner,
	dbContext catalog.DatabaseDescriptor,
	allowAdding bool,
	c nstree.Catalog,
	fn func(catalog.DatabaseDescriptor, catalog.SchemaDescriptor, catalog.TypeDescriptor, tableLookupFn) error,
) error {
	lCtx := newInternalLookupCtx(c.OrderedDescriptors(), dbContext)

	for _, typID := range lCtx.typIDs {
		typDesc := lCtx.typDescs[typID]
		if typDesc.Dropped() {
			continue
		}
		dbDesc, err := lCtx.getDatabaseByID(typDesc.GetParentID())
		if err != nil {
			return err
		}
		canSeeDescriptor, err := userCanSeeDescriptor(ctx, p, typDesc, dbDesc, allowAdding)
		if err != nil {
			return err
		}
		if !canSeeDescriptor {
			continue
		}
		sc, err := lCtx.getSchemaByID(typDesc.GetParentSchemaID())
		if err != nil {
			return err
		}
		if err := fn(dbDesc, sc, typDesc, lCtx); err != nil {
			return err
		}
	}
	return nil
}

func forEachTableDescWithTableLookupInternalFromDescriptors(
	ctx context.Context,
	p *planner,
	dbContext catalog.DatabaseDescriptor,
	virtualOpts virtualOpts,
	allowAdding bool,
	c nstree.Catalog,
	fn func(catalog.DatabaseDescriptor, catalog.SchemaDescriptor, catalog.TableDescriptor, tableLookupFn) error,
) error {
	lCtx := newInternalLookupCtx(c.OrderedDescriptors(), dbContext)

	if virtualOpts == virtualMany || virtualOpts == virtualCurrentDB {
		// Virtual descriptors first.
		vt := p.getVirtualTabler()
		vEntries := vt.getSchemas()
		vSchemaOrderedNames := vt.getSchemaNames()
		iterate := func(dbDesc catalog.DatabaseDescriptor) error {
			for _, virtSchemaName := range vSchemaOrderedNames {
				virtSchemaEntry := vEntries[virtSchemaName]
				for _, tName := range virtSchemaEntry.orderedDefNames {
					te := virtSchemaEntry.defs[tName]
					if err := fn(dbDesc, virtSchemaEntry.desc, te.desc, lCtx); err != nil {
						return err
					}
				}
			}
			return nil
		}

		switch virtualOpts {
		case virtualCurrentDB:
			if err := iterate(dbContext); err != nil {
				return err
			}
		case virtualMany:
			for _, dbID := range lCtx.dbIDs {
				dbDesc := lCtx.dbDescs[dbID]
				if err := iterate(dbDesc); err != nil {
					return err
				}
			}
		}
	}

	// Physical descriptors next.
	for _, tbID := range lCtx.tbIDs {
		table := lCtx.tbDescs[tbID]
		dbDesc, parentExists := lCtx.dbDescs[table.GetParentID()]
		canSeeDescriptor, err := userCanSeeDescriptor(ctx, p, table, dbDesc, allowAdding)
		if err != nil {
			return err
		}
		if table.Dropped() || !canSeeDescriptor {
			continue
		}
		var sc catalog.SchemaDescriptor
		if parentExists {
			sc, err = lCtx.getSchemaByID(table.GetParentSchemaID())
			if err != nil && !table.IsTemporary() {
				return err
			} else if table.IsTemporary() {
				// Look up the schemas for this database if we discover that there is a
				// missing temporary schema name. Temporary schemas have namespace
				// entries. The below code will go and lookup schema names from the
				// namespace table if needed to qualify the name of a temporary table.
				if err := forEachSchema(ctx, p, dbDesc, false /* requiresPrivileges*/, func(schema catalog.SchemaDescriptor) error {
					if schema.GetID() != table.GetParentSchemaID() {
						return nil
					}
					_, exists, err := lCtx.GetSchemaName(ctx, schema.GetID(), dbDesc.GetID(), p.ExecCfg().Settings.Version)
					if err != nil || exists {
						return err
					}
					sc = schema
					lCtx.schemaNames[sc.GetID()] = sc.GetName()
					lCtx.schemaDescs[sc.GetID()] = sc
					lCtx.schemaIDs = append(lCtx.schemaIDs, sc.GetID())
					return nil
				}); err != nil {
					return errors.Wrapf(err, "failed to look up schema id %d", table.GetParentSchemaID())
				}
				if sc == nil {
					sc = schemadesc.NewTemporarySchema(catconstants.PgTempSchemaName, table.GetParentSchemaID(), dbDesc.GetID())
				}
			}
		}
		if err := fn(dbDesc, sc, table, lCtx); err != nil {
			return err
		}
	}
	return nil
}

type roleOptions struct {
	*tree.DJSON
}

func (r roleOptions) noLogin() (tree.DBool, error) {
	nologin, err := r.Exists("NOLOGIN")
	return tree.DBool(nologin), err
}

func (r roleOptions) validUntil(p *planner) (tree.Datum, error) {
	const validUntilKey = "VALID UNTIL"
	jsonValue, err := r.FetchValKey(validUntilKey)
	if err != nil {
		return nil, err
	}
	if jsonValue == nil {
		return tree.DNull, nil
	}
	validUntilText, err := jsonValue.AsText()
	if err != nil {
		return nil, err
	}
	if validUntilText == nil {
		return tree.DNull, nil
	}
	validUntil, _, err := pgdate.ParseTimestamp(
		p.EvalContext().GetRelativeParseTime(),
		pgdate.DefaultDateStyle(),
		*validUntilText,
	)
	if err != nil {
		return nil, errors.Errorf("rolValidUntil string %s could not be parsed with datestyle %s", *validUntilText, p.EvalContext().GetDateStyle())
	}
	return tree.MakeDTimestampTZ(validUntil, time.Second)
}

func (r roleOptions) createDB() (tree.DBool, error) {
	createDB, err := r.Exists("CREATEDB")
	return tree.DBool(createDB), err
}

func (r roleOptions) createRole() (tree.DBool, error) {
	createRole, err := r.Exists("CREATEROLE")
	return tree.DBool(createRole), err
}

func forEachRoleQuery(ctx context.Context, p *planner) string {
	return `
SELECT
	u.username,
	"isRole",
  drs.settings,
	json_object_agg(COALESCE(ro.option, 'null'), ro.value)
FROM
	system.users AS u
	LEFT JOIN system.role_options AS ro ON
			ro.username = u.username
  LEFT JOIN system.database_role_settings AS drs ON 
			drs.role_name = u.username AND drs.database_id = 0
GROUP BY
	u.username, "isRole", drs.settings;
`
}

func forEachRole(
	ctx context.Context,
	p *planner,
	fn func(userName username.SQLUsername, isRole bool, options roleOptions, settings tree.Datum) error,
) error {
	query := forEachRoleQuery(ctx, p)

	// For some reason, using the iterator API here causes privilege_builtins
	// logic test fail in 3node-tenant config with 'txn already encountered an
	// error' (because of the context cancellation), so we buffer all roles
	// first.
	rows, err := p.ExtendedEvalContext().ExecCfg.InternalExecutor.QueryBuffered(
		ctx, "read-roles", p.txn, query,
	)
	if err != nil {
		return err
	}

	for _, row := range rows {
		usernameS := tree.MustBeDString(row[0])
		isRole, ok := row[1].(*tree.DBool)
		if !ok {
			return errors.Errorf("isRole should be a boolean value, found %s instead", row[1].ResolvedType())
		}

		defaultSettings := row[2]
		roleOptionsJSON, ok := row[3].(*tree.DJSON)
		if !ok {
			return errors.Errorf("roleOptionJson should be a JSON value, found %s instead", row[3].ResolvedType())
		}
		options := roleOptions{roleOptionsJSON}

		// system tables already contain normalized usernames.
		userName := username.MakeSQLUsernameFromPreNormalizedString(string(usernameS))
		if err := fn(userName, bool(*isRole), options, defaultSettings); err != nil {
			return err
		}
	}

	return nil
}

func forEachRoleMembership(
	ctx context.Context,
	ie sqlutil.InternalExecutor,
	txn *kv.Txn,
	fn func(role, member username.SQLUsername, isAdmin bool) error,
) (retErr error) {
	const query = `SELECT "role", "member", "isAdmin" FROM system.role_members`
	it, err := ie.QueryIterator(ctx, "read-members", txn, query)
	if err != nil {
		return err
	}
	// We have to make sure to close the iterator since we might return from the
	// for loop early (before Next() returns false).
	defer func() { retErr = errors.CombineErrors(retErr, it.Close()) }()

	var ok bool
	for ok, err = it.Next(ctx); ok; ok, err = it.Next(ctx) {
		row := it.Cur()
		roleName := tree.MustBeDString(row[0])
		memberName := tree.MustBeDString(row[1])
		isAdmin := row[2].(*tree.DBool)

		// The names in the system tables are already normalized.
		if err := fn(
			username.MakeSQLUsernameFromPreNormalizedString(string(roleName)),
			username.MakeSQLUsernameFromPreNormalizedString(string(memberName)),
			bool(*isAdmin)); err != nil {
			return err
		}
	}
	return err
}

func userCanSeeDescriptor(
	ctx context.Context, p *planner, desc, parentDBDesc catalog.Descriptor, allowAdding bool,
) (bool, error) {
	if !descriptorIsVisible(desc, allowAdding) {
		return false, nil
	}

	// TODO(richardjcai): We may possibly want to remove the ability to view
	// the descriptor if they have any privilege on the descriptor and only
	// allow the descriptor to be viewed if they have CONNECT on the DB. #59827.
	canSeeDescriptor := p.CheckAnyPrivilege(ctx, desc) == nil
	// Users can see objects in the database if they have connect privilege.
	if parentDBDesc != nil {
		canSeeDescriptor = canSeeDescriptor || p.CheckPrivilege(ctx, parentDBDesc, privilege.CONNECT) == nil
	}
	return canSeeDescriptor, nil
}

func descriptorIsVisible(desc catalog.Descriptor, allowAdding bool) bool {
	return desc.Public() || (allowAdding && desc.Adding())
}
