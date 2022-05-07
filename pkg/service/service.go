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

func getUserDetails(ctrl controller.Ctrl) http.HandlerFunc{
	return func(w http.ResponseWriter, r *http.Request){
		vars := mux.Vars(r)
		userId := vars["id"]
		ctx := r.context()
		userDetails, err := ctrl.FindUserDetails(userId, ctx)
		if ctx.Err() != nil{
			return
		}
	}
}

func injectUser(ctrl Controller.Ctrl) http.HandlerFunc{
	return func(w http.ResponseWriter, r *http.Request){
		var ur model.User
		ctx := r.Context()
		err := json.NewDecoder(r.Body).Decode(&ur)

		id, err := ctrl.IngestUser(ur, ctx)
		if ctx.Err() != nil {
			return
		}
	}
}
