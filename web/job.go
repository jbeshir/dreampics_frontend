package web

import (
	"html/template"
	"net/http"

	"appengine"
	"appengine/blobstore"
	"appengine/datastore"

	"job"
)

var (
	jobTemplate = template.Must(template.ParseFiles("web/job.html"))
)

func init() {
	http.HandleFunc("/job/input/", jobInputHandler)
	http.HandleFunc("/job/output/", jobOutputHandler)
	http.HandleFunc("/job/", jobHandler)
}

func jobInputHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	jobID := path[11:]

	c := appengine.NewContext(r)

	state := &job.State{ID: jobID}
	if err := datastore.Get(c, state.GetKey(c), state); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	if state.InputData == "" {
		http.Error(w, "No processing input", http.StatusBadRequest)
	}

	blobKey, err := blobstore.BlobKeyForFile(c, state.InputData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.Header().Set("Cache-Control", "public,max-age:60000")
	blobstore.Send(w, blobKey)
}

func jobOutputHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	jobID := path[12:]

	c := appengine.NewContext(r)

	state := &job.State{ID: jobID}
	if err := datastore.Get(c, state.GetKey(c), state); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	if state.OutputData == "" {
		http.Error(w, "No processing output", http.StatusBadRequest)
	}

	blobKey, err := blobstore.BlobKeyForFile(c, state.OutputData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.Header().Set("Cache-Control", "public,max-age:60000")
	blobstore.Send(w, blobKey)
}

func jobHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	jobID := path[5:]

	c := appengine.NewContext(r)

	state := &job.State{ID: jobID}
	if err := datastore.Get(c, state.GetKey(c), state); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	err := jobTemplate.Execute(w, &struct{
		JobID string
		StatusDescription string
		ShowInputImage bool
		ShowOutputImage bool
	}{
		jobID,
		state.Status.Description(),
		true,
		state.Status.OutputReady(),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
