package job

import (
	"math/rand"
	"time"

	"appengine"
	"appengine/datastore"
	"appengine/delay"
)

var processJobDelay = delay.Func("processJob", processJob)

func processJob(c appengine.Context, jobID string) (err error) {

	task := TaskNone
	taskState := &taskState{}
	state := &State{ID: jobID}
	for task != TaskHaltProcessing {

		// Run the next state of transactional processing of the job,
		// to find out the next task to do and update records.
		err = datastore.RunInTransaction(c, func(c appengine.Context) error {

			// Update our copy of the job's state.
			if err := datastore.Get(c, state.GetKey(c), state); err != nil {
				return err
			}

			// Process it and get the next task to peform.
			var err error
			task, err = state.process(c, taskState)
			if err != nil {
				return err
			}

			return nil
		}, &datastore.TransactionOptions{
			XG: true,
		})

		// If there was an error, return.
		if err != nil {
			return
		}

		// If we've been given a non-transactional processing
		// task to perform, perform it. If it fails, bail out.
		// We'll retry later. Otherwise passing past here
		// indicates success.
		if err = state.doTask(c, task, taskState); err != nil {
			return
		}
	}

	return
}

// Records the state of a current job.
type State struct {

	// The unique ID of this job.
	// Used as a client token when launching the instance.
	ID string

	// The current status of the job.
	// Indicates what stage of processing it has reached.
	Status Status

	// The cloud storage object of the data uploaded for this job.
	InputData string

	// The cloud storage object of the result of this job.
	OutputData string

	// The instance assigned to this job.
	Instance Instance
}

func Create(c appengine.Context, inputData string) (id string, err error) {

	id, err = generateRandStr(64)
	if err != nil {
		return
	}

	// Create our job's state object.
	state := &State{
		ID:        id,
		Status:    StatusNew,
		InputData: inputData,
	}

	// Save the state object and schedule processing of the job.
	err = datastore.RunInTransaction(c, func(c appengine.Context) error {

		// Save the new job.
		datastore.Put(c, state.GetKey(c), state)

		// Schedule processing.
		processJobDelay.Call(c, id)
		return nil
	}, nil)
	if err != nil {
		state = nil
	}

	return
}

func (s *State) GetKey(c appengine.Context) *datastore.Key {
	return datastore.NewKey(c, "Job", s.ID, 0, nil)
}

// Process the next stage in the job.
// Must be run in a transaction.
// Returns the next task to perform for processing.
// If this is TaskHaltProcessing, this should not be called again.
// If this is TaskNone, this should be called again with taskSuccess set to false.
// Otherwise, the task must be performed, then this called with taskSuccess set to true.
func (s *State) process(c appengine.Context, taskState *taskState) (
	task Task, err error) {

	var putKeys []*datastore.Key
	var putData []interface{}

	switch s.Status {

	case StatusDone:
		fallthrough
	case StatusFailed:
		return TaskHaltProcessing, nil

	case StatusNew:
		if taskState.PoolInstances == nil {
			return TaskGetPoolInstances, err
		}

		if len(taskState.PoolInstances) == 0 {
			cert, privKey, err := generateCert()
			if err != nil {
				return TaskNone, err
			}

			authCode, err := generateRandStr(64)
			if err != nil {
				return TaskNone, err
			}

			s.Instance.Certificate = cert
			s.Instance.PrivateKey = privKey
			s.Instance.AuthCode = authCode
			s.changeStatus(StatusMustLaunchInstance, c, &putKeys, &putData)
		} else {

			var poolInstance PoolInstance
			for i := 0; i < 5; i++ {
				instanceKey := taskState.PoolInstances[
					rand.Intn(len(taskState.PoolInstances))]

				if err = datastore.Get(c, instanceKey, &poolInstance); err != nil {
					if err == datastore.ErrNoSuchEntity {
						continue
					}
					return TaskNone, err
				}

				if err = datastore.Delete(c, instanceKey); err != nil {
					return TaskNone, err
				}

				break
			}

			s.Instance = poolInstance.Instance
			s.changeStatus(StatusHaveInstance, c, &putKeys, &putData)
		}

	case StatusMustLaunchInstance:
		if err = s.Instance.launch(c, s.ID); err != nil {
			return TaskNone, err
		}
		s.changeStatus(StatusLaunchingInstance, c, &putKeys, &putData)

	case StatusLaunchingInstance:
		if !taskState.LivenessChecked {
			return TaskCheckLiveness, nil
		}
		if !taskState.LivenessCheckSuccess {
			terminateInstanceDelay.Call(c, s.Instance.ID)
			s.changeStatus(StatusFailed, c, &putKeys, &putData)
			break
		}
		s.Instance.IP = taskState.LivenessCheckPublicIP
		s.changeStatus(StatusHaveInstance, c, &putKeys, &putData)

	case StatusHaveInstance:
		if !taskState.DreamDone {
			return TaskDream, nil
		}
		s.OutputData = taskState.DreamOutputData
		s.changeStatus(StatusFinishedWithInstance, c, &putKeys, &putData)

	case StatusFinishedWithInstance:
		poolInstance := s.Instance.toPoolInstance(c)
		poolInstanceKey := datastore.NewKey(c, "PoolInstance", poolInstance.Instance.ID, 0, nil)
		putKeys = append(putKeys, poolInstanceKey)
		putData = append(putData, poolInstance)
		s.changeStatus(StatusDone, c, &putKeys, &putData)
	}

	putKeys = append(putKeys, s.GetKey(c))
	putData = append(putData, s)

	_, err = datastore.PutMulti(c, putKeys, putData)

	return TaskNone, err
}

func (s *State) changeStatus(newStatus Status,
	c appengine.Context,
	putKeys *[]*datastore.Key,
	putData *[]interface{}) {

	log := &JobLog{
		NewStatus:  newStatus,
		PrevStatus: s.Status,
		Time:       time.Now(),
	}
	logKey := datastore.NewIncompleteKey(c, "JobLog", s.GetKey(c))

	*putKeys = append(*putKeys, logKey)
	*putData = append(*putData, log)

	s.Status = newStatus
}


// Records a job's change of status.
// Stored as a child entity of the job's state.
type JobLog struct {
	NewStatus Status

	PrevStatus Status

	Time time.Time
}
