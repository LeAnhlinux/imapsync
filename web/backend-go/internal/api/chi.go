package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// chiURLParam exists so the WebSocket handler, which is registered outside the
// authenticated group, reads path params the same way as everything else.
func chiURLParam(r *http.Request, key string) string { return chi.URLParam(r, key) }
