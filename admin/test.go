package admin

import (
	"html/template"
	"net/http"
	"net/url"

	"appengine"

	"job"
	"storage"
)

var (
	testTemplate = template.Must(template.ParseFiles("admin/test.html"))
)


func init() {
	http.HandleFunc("/admin/test", testHandler)
	http.HandleFunc("/job/create", jobCreateHandler)
}

func testHandler(w http.ResponseWriter, r *http.Request) {

	c := appengine.NewContext(r)

	imageUploadUrl, err := storage.GetUploadURL(c, "/job/create")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	err = testTemplate.Execute(w, struct {
		JobCreateURL *url.URL
	}{
		imageUploadUrl,
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func jobCreateHandler(w http.ResponseWriter, r *http.Request) {

	storageName, _, err := storage.HandleUpload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	c := appengine.NewContext(r)
	id, err := job.Create(c, storageName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/job/" + id, http.StatusFound)
}
