package job

type Status int

func (status Status) Description() string {
	switch status {
	case StatusNew:
		return "Looking for free dream server..."
	case StatusMustLaunchInstance:
	case StatusLaunchingInstance:
		return "Launching dream server..."
	case StatusHaveInstance:
		return "Dreaming..."
	case StatusFinishedWithInstance:
	case StatusDone:
		return "Finished."
	case StatusFailed:
		return "Failed to process image."
	}

	return "Status is unknown."
}

func (status Status) OutputReady() bool {
	switch status {
	case StatusFinishedWithInstance:
		return true
	case StatusDone:
		return true
	}

	return false
}

const (
	StatusNew Status = iota
	StatusMustLaunchInstance
	StatusLaunchingInstance
	StatusHaveInstance
	StatusFinishedWithInstance
	StatusDone
	StatusFailed
)

