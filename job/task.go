package job

import (
	"errors"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"appengine"
	"appengine/datastore"

	"storage"
)

type Task int

const (
	TaskNone Task = iota
	TaskHaltProcessing
	TaskGetPoolInstances
	TaskCheckLiveness
	TaskDream
)

type taskState struct {
	PoolInstances []*datastore.Key
	PoolInstancesRetrievedBefore bool
	LivenessChecked bool
	LivenessCheckSuccess bool
	LivenessCheckPublicIP string
	DreamDone bool
	DreamOutputData string
}

// Run a given non-transactional task as part of processing a job.
// Updates taskState to record results.
func (s *State) doTask(c appengine.Context, task Task, taskState *taskState) (err error) {
	switch task {

	// If in order to do processing, we've been told we need
	// to get and provide some candidate pool instances, do so.
	case TaskGetPoolInstances:
		taskState.PoolInstances, err = getCandidatePoolInstances(c,
			taskState.PoolInstancesRetrievedBefore)
		if err != nil {
			return err
		}
		taskState.PoolInstancesRetrievedBefore = true

	// If we've been asked to check the liveness of the instance,
	// do so. If it doesn't respond, wait a while and retry a number of times.
	// After that the recorded launch time becomes over thirty minutes ago,
	// we give up and fail the check, otherwise we let the task retry.
	case TaskCheckLiveness:

		for i := 0; i < 6; i++ {

			// Sleep five seconds before doing the liveness check.
			time.Sleep(5 * time.Second)

			resp, checkErr := s.Instance.get(c, "dream")
			if checkErr == nil {
				resp.Body.Close()
				taskState.LivenessChecked = true
				taskState.LivenessCheckSuccess = true
				taskState.LivenessCheckPublicIP = strings.Split(
					resp.Request.URL.Host, ":")[0]
				break
			}
		}

		if !taskState.LivenessCheckSuccess {
			if time.Now().Add(-30 * time.Minute).After(s.Instance.LaunchTime) {
				taskState.LivenessChecked = true
				return nil
			} else {
				return errors.New("Gave up liveness checks, try again later.")
			}
		}

	case TaskDream:

		// We need to read the input data, so we can send it to the dream server.
		inputData, err := storage.ReadFile(c, s.InputData)
		if err != nil {
			return err
		}

		// Try to run the processing job.
		resp, err := s.Instance.postFile(c, "dream?auth_code=" + s.Instance.AuthCode,
			"image", inputData)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return errors.New("Processing job failed: " + resp.Status)
		}

		resultData, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()

		// Now we've done the processing and gotten the result image, save it to storage.
		outputName := "job/" + s.ID + "/output"
		outputDataPath, err := storage.WriteFile(c, outputName, resultData)
		if err != nil {
			return err
		}

		taskState.DreamDone = true
		taskState.DreamOutputData = outputDataPath
	}

	return nil
}
