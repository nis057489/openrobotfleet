package controller

import (
	"log"
	"net/http"
)

func (c *Controller) ListJobs(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("robot")
	jobs, err := c.DB.ListJobs(r.Context(), target)
	if err != nil {
		log.Printf("list jobs: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}
	respondJSON(w, http.StatusOK, jobs)
}
