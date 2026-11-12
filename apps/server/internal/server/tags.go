package server

import (
	"github.com/go-chi/chi/v5"
	"net/http"
)

func (a *API) library(w http.ResponseWriter, r *http.Request) {
	data, err := a.Store.Library(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}
func (a *API) saveTag(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, 400, err)
		return
	}
	id := chi.URLParam(r, "tagID")
	create := r.Method == http.MethodPost
	if create {
		var err error
		id, err = newID()
		if err != nil {
			a.fail(w, err)
			return
		}
	}
	if err := a.Store.SaveTag(r.Context(), id, in.Name, create); err != nil {
		writeError(w, 400, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) deleteTag(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteTag(r.Context(), chi.URLParam(r, "tagID")); err != nil {
		writeError(w, 400, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) saveTagGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string   `json:"name"`
		TagIDs []string `json:"tagIds"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, 400, err)
		return
	}
	id := chi.URLParam(r, "groupID")
	create := r.Method == http.MethodPost
	if create {
		var err error
		id, err = newID()
		if err != nil {
			a.fail(w, err)
			return
		}
	}
	if err := a.Store.SaveTagGroup(r.Context(), id, in.Name, in.TagIDs, create); err != nil {
		writeError(w, 400, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) deleteTagGroup(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteTagGroup(r.Context(), chi.URLParam(r, "groupID")); err != nil {
		writeError(w, 400, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) setTrackTags(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TrackIDs []string `json:"trackIds"`
		TagIDs   []string `json:"tagIds"`
		Mode     string   `json:"mode"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, 400, err)
		return
	}
	if err := a.Store.SetTrackTags(r.Context(), in.TrackIDs, in.TagIDs, in.Mode); err != nil {
		writeError(w, 400, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
