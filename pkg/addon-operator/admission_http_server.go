package addon_operator

import (
	"fmt"
	"net/http"

	"github.com/flant/addon-operator/pkg/module_manager/apis/v1alpha1"
)

func aaa() {
	mux := http.NewServeMux()
	mux.Handle("/validate/v1alpha1/modules", v1alpha1.ValidationHandler())

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", 9651),
		Handler: mux,
	}

	err := srv.ListenAndServeTLS("/certs/tls.crt", "/certs/tls.key")
	if err != nil {
		fmt.Println("TMP ERR", err)
	}
}