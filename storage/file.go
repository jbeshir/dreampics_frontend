package storage

import (
	"io/ioutil"
	"strings"

	"appengine"

	"google.golang.org/cloud/storage"
)

func ReadFile(c appengine.Context, gsPath string) (data []byte, err error) {
	ctx, err := getGcsContext(c)
	if err != nil {
		return nil, err
	}

	filename := strings.SplitN(gsPath, "/", 4)[3]

	rc, err := storage.NewReader(ctx, gcsBucket, filename)
	if err != nil {
		return nil, err
	}

	data, err = ioutil.ReadAll(rc)
	rc.Close()
	return
}

func WriteFile(c appengine.Context, filename string, data []byte) (gsPath string, err error) {
	ctx, err := getGcsContext(c)
	if err != nil {
		return "", err
	}

	wc := storage.NewWriter(ctx, gcsBucket, filename)
	wc.ContentType = "image/png"

	if _, err = wc.Write(data); err != nil {
		return "", err
	}

	if err = wc.Close(); err != nil {
		return "", err
	}

	return "/gs/" + gcsBucket + "/" + filename, nil
}
