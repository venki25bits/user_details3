package service

import (
	"user-details/pkg/controller"
	"net/http"

	router "vendor.lib/tng/tng-lib/router/mux"
)

func AddHandlers(r *router.Router, ctrl *controller.Controller) {
	r.Handle("/ready", ready(ctrl)).Methods(http.MethodGet, http.MethodHead)
}

func ready(ctrl *controller.Controller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		err := ctrl.Ready()
		if err != nil {
			router.RespondWithError(w, http.StatusServiceUnavailable, err)
		} else {
			router.Respond(w, http.StatusOK, []byte(http.StatusText(http.StatusOK)))
		}
	}
}