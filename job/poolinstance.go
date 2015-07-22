package job

import (
	"net/http"
	"time"

	"appengine"
	"appengine/datastore"
	"appengine/delay"
	"appengine/urlfetch"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type PoolInstance struct {

	// Data describing the instance.
	Instance Instance

	// The time the instance was added to the pool.
	PoolAddTime time.Time
}

func init() {
	http.HandleFunc("/job/cron/shrink_pool", shrinkPoolHandler)
}

func shrinkPoolHandler(w http.ResponseWriter, r *http.Request) {

	c := appengine.NewContext(r)
	if err := ShrinkPool(c); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func ShrinkPool(c appengine.Context) (err error) {

	done := false
	var cursor *datastore.Cursor
	for !done {
		// Query for long-idle pool instances.
		// We require them be unused for 15 minutes.
		// 
		// In order to actually keep our instance count low as possible
		// this relies on us preferentially using instances whose
		// pool add time was as high as possible, so we lean towards
		// a smaller number of very active instances.
		maxPoolAddTime := time.Now().Add(-time.Minute * 15)
		q := datastore.NewQuery("PoolInstance")
		q = q.Filter("PoolAddTime <", maxPoolAddTime)
		if cursor != nil {
			q.Start(*cursor)
		}
		q = q.KeysOnly()

		candidateKeys := []*datastore.Key(nil)
		if err != nil {
			return err
		}

		i := q.Run(c)
		cursor = nil
		for {

			candidateKey, err := i.Next(c)
			if err != nil {
				if err == datastore.Done {
					break
				}
				return err
			}

			candidateKeys = append(candidateKeys, candidateKey)
			if len(candidateKeys) >= 1000 {
				cursor = new(datastore.Cursor)
				*cursor, err = i.Cursor()
				if err != nil {
					return err
				}
				break
			}
		}



		// Go over each possibly ready to be terminated pool instance,
		// and in a transaction, check it still meets our
		// criteria for removal, and if so, remove it.
		for _, candidateKey := range candidateKeys {

			// If we fail to act on a given candidate,
			// we will just ignore them and try again
			// next time we try to shrink the pool.
			_ = datastore.RunInTransaction(c, func(c appengine.Context) error {
				var p PoolInstance
				if err = datastore.Get(c, candidateKey, &p); err != nil {
					if err == datastore.ErrNoSuchEntity {
						return nil
					} else {
						return err
					}
				}

				if p.PoolAddTime.After(maxPoolAddTime) {
					return nil
				}

				// If the instance was launched less than 50 minutes ago,
				// skip it. Due to Amazon's pricing model, starting an
				// instance already pays for the first hour,
				// so we may as well keep it in the pool in case we need it.
				maxLaunchTime := time.Now().Add(-time.Minute * 50)
				if p.Instance.LaunchTime.After(maxLaunchTime) {
					return nil
				}

				if err = datastore.Delete(c, candidateKey); err != nil {
					return err
				}
				terminateInstanceDelay.Call(c, p.Instance.ID)

				return nil
			}, nil)
		}

		done = cursor == nil;
	}

	return nil
}

var terminateInstanceDelay = delay.Func("terminatePoolInstance",
	terminateInstance)

func terminateInstance(c appengine.Context, id string) error {

	var awsConfig = &aws.Config{
		HTTPClient: urlfetch.Client(c),
	}
	svc := ec2.New(awsConfig)

	params := &ec2.TerminateInstancesInput{
		InstanceIDs: []*string{ aws.String(id) },
	}

	_, err := svc.TerminateInstances(params)
	return err
}

func getCandidatePoolInstances(c appengine.Context, retry bool) (keys []*datastore.Key,
	err error) {

	// Get the most recently added pool instances.
	// We only care about the PoolAddTime property.
	q := datastore.NewQuery("PoolInstance").
		Project("PoolAddTime").
		Order("-PoolAddTime").
		Limit(100)

	var instances []PoolInstance
	queryKeys, err := q.GetAll(c, &instances)
	if err != nil {
		return nil, err
	}

	// If we have no pool instances, just return.
	// We must NOT return nil, so it isn't mistaken for an unset value.
	if len(instances) < 1 {
		return make([]*datastore.Key, 0), nil
	}

	maxTimeDifference := 5 * time.Second
	if retry {
		maxTimeDifference *= 5
	}
	minAddTime := instances[0].PoolAddTime.Add(-maxTimeDifference)

	count := len(instances)
	for i, v := range instances {
		if minAddTime.After(v.PoolAddTime) {
			count = i
			break
		}
	}


	keys = make([]*datastore.Key, count)
	for i := 0; i < count; i++ {
		keys[i] = datastore.NewKey(c, "PoolInstance",
			queryKeys[i].StringID(), 0, nil)
	}

	return
}
