// Copyright 2022 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.
import {
  TransactionInsightDetails,
  TransactionInsightDetailsStateProps,
  TransactionInsightDetailsDispatchProps,
} from "@cockroachlabs/cluster-ui";
import { connect } from "react-redux";
import { RouteComponentProps, withRouter } from "react-router-dom";
import { refreshTransactionInsightDetails } from "src/redux/apiReducers";
import { AdminUIState } from "src/redux/state";
import {
  selectTxnInsightDetails,
  selectTransactionInsightDetailsError,
} from "src/views/insights/insightsSelectors";
import { setGlobalTimeScaleAction } from "src/redux/statements";
import { selectTimeScale } from "src/redux/timeScale";

const mapStateToProps = (
  state: AdminUIState,
  props: RouteComponentProps,
): TransactionInsightDetailsStateProps => {
  return {
    insightDetails: selectTxnInsightDetails(state, props),
    insightError: selectTransactionInsightDetailsError(state, props),
    timeScale: selectTimeScale(state),
  };
};

const mapDispatchToProps: TransactionInsightDetailsDispatchProps = {
  refreshTransactionInsightDetails: refreshTransactionInsightDetails,
  setTimeScale: setGlobalTimeScaleAction,
};

const TransactionInsightDetailsPage = withRouter(
  connect<
    TransactionInsightDetailsStateProps,
    TransactionInsightDetailsDispatchProps,
    RouteComponentProps
  >(
    mapStateToProps,
    mapDispatchToProps,
  )(TransactionInsightDetails),
);

export default TransactionInsightDetailsPage;
