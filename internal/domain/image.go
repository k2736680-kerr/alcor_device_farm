package domain

import "time"

type ImageStatus string

const (
	ImageDraft      ImageStatus = "draft"
	ImageValidating ImageStatus = "validating"
	ImageReady      ImageStatus = "ready"
	ImageFailed     ImageStatus = "failed"
	ImageDisabled   ImageStatus = "disabled"
)

var imageTransitions = map[ImageStatus]map[ImageStatus]struct{}{
	ImageDraft:      allowed(ImageValidating, ImageDisabled),
	ImageValidating: allowed(ImageDraft, ImageReady, ImageFailed, ImageDisabled),
	ImageReady:      allowed(ImageDraft, ImageValidating, ImageDisabled),
	ImageFailed:     allowed(ImageDraft, ImageValidating, ImageDisabled),
	ImageDisabled:   allowed(ImageDraft),
}

type Image struct{ state stateMachine[ImageStatus] }

func NewImage(id string) (*Image, error) { return RestoreImage(id, ImageDraft) }

// RestoreImage rebuilds an aggregate from a persisted row; it does not mutate an existing Image.
func RestoreImage(id string, status ImageStatus) (*Image, error) {
	state, err := newStateMachine("device_image", id, "status", status, validImageStatus, imageTransitions)
	if err != nil {
		return nil, err
	}
	return &Image{state: state}, nil
}

func (image *Image) ID() string          { return image.state.idValue() }
func (image *Image) Status() ImageStatus { return image.state.statusValue() }
func (image *Image) Transition(to ImageStatus, reason string, at time.Time) error {
	return image.state.transition(to, reason, at)
}
func (image *Image) Events() []TransitionEvent { return image.state.eventsCopy() }
func (image *Image) ClearEvents()              { image.state.clearEvents() }

func validImageStatus(status ImageStatus) bool {
	_, ok := imageTransitions[status]
	return ok
}
