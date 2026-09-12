package storage_test

import (
	"testing"

	"image-gallery/internal/platform/storage/storetest"
)

func TestMemStoreContract(t *testing.T) { storetest.Run(t, storetest.NewMemStore()) }
