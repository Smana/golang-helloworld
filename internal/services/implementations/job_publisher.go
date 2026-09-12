package implementations

import (
	"context"

	"image-gallery/internal/domain/image"
	"image-gallery/internal/platform/queue"
)

type queueJobPublisher struct{ p *queue.Producer }

// NewQueueJobPublisher publishes processing jobs on the Valkey stream.
func NewQueueJobPublisher(p *queue.Producer) image.JobPublisher { return queueJobPublisher{p: p} }

func (q queueJobPublisher) PublishProcessImage(ctx context.Context, imageID int, objectKey string) error {
	_, err := q.p.Publish(ctx, queue.Job{Type: queue.JobTypeProcessImage, ImageID: imageID, ObjectKey: objectKey})
	return err
}
