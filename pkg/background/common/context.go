package common

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	kyverno "github.com/kyverno/kyverno/api/kyverno/v1"
	urkyverno "github.com/kyverno/kyverno/api/kyverno/v1beta1"
	"github.com/kyverno/kyverno/pkg/config"
	dclient "github.com/kyverno/kyverno/pkg/dclient"
	"github.com/kyverno/kyverno/pkg/engine"
	"github.com/kyverno/kyverno/pkg/engine/context"
	utils "github.com/kyverno/kyverno/pkg/utils"
	"github.com/pkg/errors"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func NewBackgroundContext(dclient dclient.Interface, ur *urkyverno.UpdateRequest,
	policy kyverno.PolicyInterface,
	trigger *unstructured.Unstructured,
	cfg config.Configuration,
	namespaceLabels map[string]string,
	logger logr.Logger,
) (*engine.PolicyContext, bool, error) {
	ctx := context.NewContext()
	requestString := ur.Spec.Context.AdmissionRequestInfo.AdmissionRequest
	var request admissionv1.AdmissionRequest
	var new, old unstructured.Unstructured

	if requestString != "" {
		err := json.Unmarshal([]byte(requestString), &request)
		if err != nil {
			return nil, false, errors.Wrap(err, "error parsing the request string")
		}

		if err := ctx.AddRequest(&request); err != nil {
			return nil, false, errors.Wrap(err, "failed to load request in context")
		}

		new, old, err = utils.ExtractResources(nil, &request)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to load request in context")
		}

		if !reflect.DeepEqual(new, unstructured.Unstructured{}) {
			if !check(&new, trigger) {
				err := fmt.Errorf("resources don't match")
				return nil, false, errors.Wrapf(err, "resource %v", ur.Spec.Resource)
			}
		}
	}

	if trigger == nil {
		trigger = &old
	}

	if trigger == nil {
		return nil, false, errors.New("trigger resource does not exist")
	}

	err := ctx.AddResource(trigger.Object)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to load resource in context")
	}

	err = ctx.AddOldResource(old.Object)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to load resource in context")
	}

	err = ctx.AddUserInfo(ur.Spec.Context.UserRequestInfo)
	if err != nil {
		return nil, false, errors.Wrapf(err, "failed to load SA in context")
	}

	err = ctx.AddServiceAccount(ur.Spec.Context.UserRequestInfo.AdmissionUserInfo.Username)
	if err != nil {
		return nil, false, errors.Wrapf(err, "failed to load UserInfo in context")
	}

	if err := ctx.AddImageInfos(trigger); err != nil {
		logger.Error(err, "unable to add image info to variables context")
	}

	policyContext := &engine.PolicyContext{
		NewResource:         *trigger,
		OldResource:         old,
		Policy:              policy,
		AdmissionInfo:       ur.Spec.Context.UserRequestInfo,
		ExcludeGroupRole:    cfg.GetExcludeGroupRole(),
		ExcludeResourceFunc: cfg.ToFilter,
		JSONContext:         ctx,
		NamespaceLabels:     namespaceLabels,
		Client:              dclient,
		AdmissionOperation:  false,
	}

	return policyContext, false, nil
}

func check(admissionRsc, existingRsc *unstructured.Unstructured) bool {
	if existingRsc == nil {
		return admissionRsc == nil
	}

	if admissionRsc.GetName() != existingRsc.GetName() {
		return false
	}
	if admissionRsc.GetNamespace() != existingRsc.GetNamespace() {
		return false
	}
	if admissionRsc.GetKind() != existingRsc.GetKind() {
		return false
	}
	if admissionRsc.GetAPIVersion() != existingRsc.GetAPIVersion() {
		return false
	}
	return true
}
