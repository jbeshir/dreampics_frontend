package storage

import (
	"errors"
	"net/http"
	"net/url"

	"appengine"
	"appengine/blobstore"
	"appengine/urlfetch"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"google.golang.org/cloud"
	"google.golang.org/cloud/storage"
)

func getGcsContext(c appengine.Context) (ctx context.Context, err error) {

	// This turns out to be the way to get the GCS client library to work 
	accessToken, _, err := appengine.AccessToken(c, storage.ScopeFullControl)
	if err != nil {
		return nil, err
	}

	hc := &http.Client{}
	hc.Transport = &oauth2.Transport{
		Base:   &urlfetch.Transport{
			Context: c,
		},
		Source: oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: accessToken,
		}),
	}

	ctx = cloud.NewContext(appengine.AppID(c), hc)
	return ctx, nil
}

func GetUploadURL(c appengine.Context, handler string) (url *url.URL, err error) {
	return blobstore.UploadURL(c, handler, &blobstore.UploadURLOptions{
		StorageBucket: gcsBucket + "/upload/",
	})
}

func HandleUpload(r *http.Request) (storageName string, other url.Values, err error) {
	blobs, other, err := blobstore.ParseUpload(r)
	if err != nil {
		return "", nil, err
	}

	// Delete any uploads other than the one we actually want.
	// Stops users from wasting our storage for no reason.
	var deleteList []string
	for k, fileList := range blobs {
		for i, file := range fileList {
			if k != "file" || i != 0 {
				deleteList = append(deleteList, file.ObjectName)
			}
		}
	}
	if len(deleteList) > 0 {

		c := appengine.NewContext(r)
		ctx, err := getGcsContext(c)
		if err != nil {
			return "", nil, err
		}

		for _, junk := range deleteList {

			// If one of our delete ops fails, still try the rest,
			// but set err aside, preserving it, so we can return
			// after.
			if newErr := storage.DeleteObject(ctx, gcsBucket, junk); err != nil {
				err = newErr
			}
		}
	}
	if err != nil {
		return "", nil, err
	}

	if len(blobs["file"]) == 0 {
		return "", nil, errors.New("No file uploaded.")
	}

	return blobs["file"][0].ObjectName, other, nil
}
