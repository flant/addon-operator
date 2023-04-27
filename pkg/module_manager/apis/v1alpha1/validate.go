package v1alpha1

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	log "github.com/sirupsen/logrus"
	kwhhttp "github.com/slok/kubewebhook/v2/pkg/http"
	kwhlogrus "github.com/slok/kubewebhook/v2/pkg/log/logrus"
	"github.com/slok/kubewebhook/v2/pkg/model"
	kwhvalidating "github.com/slok/kubewebhook/v2/pkg/webhook/validating"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var vf = kwhvalidating.ValidatorFunc(func(ctx context.Context, review *model.AdmissionReview, obj metav1.Object) (result *kwhvalidating.ValidatorResult, err error) {
	module, ok := obj.(*Module)
	if !ok {
		fmt.Println("NOT module", ok)
		fmt.Println(reflect.TypeOf(obj))
		fmt.Println(obj)
		return &kwhvalidating.ValidatorResult{}, nil
	}

	fmt.Printf("VALIDATION REVIEW: %v\n", review)

	fmt.Printf("MODULE: %v\n", module)

	return &kwhvalidating.ValidatorResult{
		Valid:   true,
		Message: "",
	}, nil
})

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
