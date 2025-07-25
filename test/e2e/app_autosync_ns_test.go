package e2e

import (
	"testing"

	. "github.com/argoproj/gitops-engine/pkg/sync/common"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v3/test/e2e/fixture"
	. "github.com/argoproj/argo-cd/v3/test/e2e/fixture/app"
	"github.com/argoproj/argo-cd/v3/util/errors"
)

func TestNSAutoSyncSelfHealDisabled(t *testing.T) {
	Given(t).
		SetTrackingMethod("annotation").
		Path(guestbookPath).
		SetAppNamespace(fixture.AppNamespace()).
		// TODO: There is a bug with annotation tracking method that prevents
		// controller from picking up changes in the cluster.
		When().
		// app should be auto-synced once created
		CreateFromFile(func(app *Application) {
			app.Spec.SyncPolicy = &SyncPolicy{Automated: &SyncPolicyAutomated{SelfHeal: false}}
		}).
		Then().
		Expect(SyncStatusIs(SyncStatusCodeSynced)).
		// app should be auto-synced if git change detected
		When().
		PatchFile("guestbook-ui-deployment.yaml", `[{"op": "replace", "path": "/spec/revisionHistoryLimit", "value": 1}]`).
		Refresh(RefreshTypeNormal).
		Then().
		Expect(SyncStatusIs(SyncStatusCodeSynced)).
		// app should not be auto-synced if k8s change detected
		When().
		And(func() {
			errors.NewHandler(t).FailOnErr(fixture.KubeClientset.AppsV1().Deployments(fixture.DeploymentNamespace()).Patch(t.Context(),
				"guestbook-ui", types.MergePatchType, []byte(`{"spec": {"revisionHistoryLimit": 0}}`), metav1.PatchOptions{}))
		}).
		Then().
		Expect(SyncStatusIs(SyncStatusCodeOutOfSync))
}

func TestNSAutoSyncSelfHealEnabled(t *testing.T) {
	Given(t).
		SetTrackingMethod("annotation").
		Path(guestbookPath).
		SetAppNamespace(fixture.AppNamespace()).
		When().
		// app should be auto-synced once created
		CreateFromFile(func(app *Application) {
			app.Spec.SyncPolicy = &SyncPolicy{
				Automated: &SyncPolicyAutomated{SelfHeal: true},
				Retry:     &RetryStrategy{Limit: 0},
			}
		}).
		Then().
		Expect(OperationPhaseIs(OperationSucceeded)).
		Expect(SyncStatusIs(SyncStatusCodeSynced)).
		When().
		// app should be auto-synced once k8s change detected
		And(func() {
			errors.NewHandler(t).FailOnErr(fixture.KubeClientset.AppsV1().Deployments(fixture.DeploymentNamespace()).Patch(t.Context(),
				"guestbook-ui", types.MergePatchType, []byte(`{"spec": {"revisionHistoryLimit": 0}}`), metav1.PatchOptions{}))
		}).
		Refresh(RefreshTypeNormal).
		Then().
		Expect(OperationPhaseIs(OperationSucceeded)).
		Expect(SyncStatusIs(SyncStatusCodeSynced)).
		When().
		// app should be attempted to auto-synced once and marked with error after failed attempt detected
		PatchFile("guestbook-ui-deployment.yaml", `[{"op": "replace", "path": "/spec/revisionHistoryLimit", "value": "badValue"}]`).
		Refresh(RefreshTypeNormal).
		Then().
		Expect(OperationPhaseIs(OperationFailed)).
		When().
		// Trigger refresh again to make sure controller notices previously failed sync attempt before expectation timeout expires
		Refresh(RefreshTypeNormal).
		Then().
		Expect(SyncStatusIs(SyncStatusCodeOutOfSync)).
		Expect(Condition(ApplicationConditionSyncError, "Failed last sync attempt")).
		When().
		// SyncError condition should be removed after successful sync
		PatchFile("guestbook-ui-deployment.yaml", `[{"op": "replace", "path": "/spec/revisionHistoryLimit", "value": 1}]`).
		Refresh(RefreshTypeNormal).
		Then().
		Expect(OperationPhaseIs(OperationSucceeded)).
		When().
		// Trigger refresh twice to make sure controller notices successful attempt and removes condition
		Refresh(RefreshTypeNormal).
		Then().
		Expect(SyncStatusIs(SyncStatusCodeSynced)).
		And(func(app *Application) {
			assert.Empty(t, app.Status.Conditions)
		})
}
