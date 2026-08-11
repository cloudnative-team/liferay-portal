package addon

import (
	"context"
	"crypto/rsa"
	"fmt"
	"io"

	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	provisioning "github.com/liferay/liferay-portal/cloud/operator/internal/provisioning"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	stateDownloaded = "Downloaded"
	stateFailed     = "Failed"
)

type Downloader interface {
	DownloadAddOn(
		context context.Context,
		downloadRequest provisioning.DownloadRequest,
		privateKey *rsa.PrivateKey,
	) (io.ReadCloser, error)
}

func Sync(
	addOns []provisioning.AddOn,
	context context.Context,
	downloader Downloader,
	environmentID string,
	existing []licensingv1alpha1.AppStatus,
	privateKey *rsa.PrivateKey,
	store Store,
) ([]licensingv1alpha1.AppStatus, error) {
	logger := logf.FromContext(context)

	existingByVirtualEntryID := make(
		map[int64]licensingv1alpha1.AppStatus, len(existing),
	)

	for _, appStatus := range existing {
		existingByVirtualEntryID[appStatus.VirtualEntryID] = appStatus
	}

	apps := make([]licensingv1alpha1.AppStatus, 0, len(addOns))

	failed := 0

	for _, addOn := range addOns {
		if isHeld(addOn, existingByVirtualEntryID, store) {
			apps = append(apps, existingByVirtualEntryID[addOn.VirtualEntryID])

			continue
		}

		error := download(
			addOn, context, downloader, environmentID, privateKey, store,
		)

		if error != nil {
			logger.Error(
				error, "Unable to download add-on",
				"productName", addOn.ProductName,
				"virtualEntryId", addOn.VirtualEntryID,
			)

			failed++

			apps = append(apps, newAppStatus(addOn, stateFailed))

			continue
		}

		logger.Info(
			"Downloaded add-on",
			"productName", addOn.ProductName,
			"virtualEntryId", addOn.VirtualEntryID,
		)

		apps = append(apps, newAppStatus(addOn, stateDownloaded))
	}

	if failed > 0 {
		return apps, fmt.Errorf(
			"addon sync: %d of %d add-ons failed to download", failed, len(addOns),
		)
	}

	return apps, nil
}

func download(
	addOn provisioning.AddOn,
	context context.Context,
	downloader Downloader,
	environmentID string,
	privateKey *rsa.PrivateKey,
	store Store,
) error {
	reader, error := downloader.DownloadAddOn(
		context,
		provisioning.DownloadRequest{
			DownloadURL:    addOn.DownloadURL,
			EnvironmentID:  environmentID,
			VirtualEntryID: addOn.VirtualEntryID,
		},
		privateKey,
	)

	if error != nil {
		return error
	}

	defer reader.Close()

	return store.Save(addOn.SHA256Checksum, reader, addOn.VirtualEntryID)
}

func isHeld(
	addOn provisioning.AddOn,
	existingByVirtualEntryID map[int64]licensingv1alpha1.AppStatus,
	store Store,
) bool {
	appStatus, ok := existingByVirtualEntryID[addOn.VirtualEntryID]

	if !ok {
		return false
	}

	if (appStatus.State != stateDownloaded) ||
		(appStatus.Checksum != addOn.SHA256Checksum) {

		return false
	}

	has, error := store.Has(addOn.SHA256Checksum, addOn.VirtualEntryID)

	if error != nil {
		return false
	}

	return has
}

func newAppStatus(
	addOn provisioning.AddOn, state string,
) licensingv1alpha1.AppStatus {
	return licensingv1alpha1.AppStatus{
		Checksum:       addOn.SHA256Checksum,
		Name:           addOn.ProductName,
		State:          state,
		VirtualEntryID: addOn.VirtualEntryID,
	}
}
