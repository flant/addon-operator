package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	log "github.com/sirupsen/logrus"
	kwhhttp "github.com/slok/kubewebhook/v2/pkg/http"
	kwhlogrus "github.com/slok/kubewebhook/v2/pkg/log/logrus"
	"github.com/slok/kubewebhook/v2/pkg/model"
	kwhvalidating "github.com/slok/kubewebhook/v2/pkg/webhook/validating"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen=false
var vf = kwhvalidating.ValidatorFunc(func(ctx context.Context, review *model.AdmissionReview, obj metav1.Object) (result *kwhvalidating.ValidatorResult, err error) {
	fmt.Println("USER", review.UserInfo.Username)

	if review.UserInfo.Username != "deckhouse-controller" {
		return &kwhvalidating.ValidatorResult{
			Valid:   false,
			Message: "Module manual delete is forbidden",
		}, nil
	}

	//module, ok := obj.(*Module)
	//if !ok {
	//	return &kwhvalidating.ValidatorResult{}, nil
	//}

	return &kwhvalidating.ValidatorResult{
		Valid:   true,
		Message: "",
	}, nil
})

// +k8s:deepcopy-gen=false

func ValidationHandler() http.Handler {
	kl := kwhlogrus.NewLogrus(log.NewEntry(log.StandardLogger()))

	// Create webhook.
	wh, _ := kwhvalidating.NewWebhook(kwhvalidating.WebhookConfig{
		ID:        "module-operations",
		Validator: vf,
		Logger:    kl,
		Obj:       &Module{},
	})

	return kwhhttp.MustHandlerFor(kwhhttp.HandlerConfig{Webhook: wh, Logger: kl})
}
