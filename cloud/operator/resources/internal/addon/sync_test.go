package addon

import (
	"bytes"
	"context"
	"crypto/rsa"
	"fmt"
	"io"
	"testing"

	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	provisioning "github.com/liferay/liferay-portal/cloud/operator/internal/provisioning"
)

func TestSync(t *testing.T) {
	addOn := provisioning.AddOn{
		DownloadURL:    "https://example.com/marketplace/virtual-entry/42",
		ProductName:    "Sample Add-on",
		SHA256Checksum: "abc123",
		VirtualEntryID: 42,
	}

	testCases := map[string]struct {
		downloadErrors map[int64]error
		existing       []licensingv1alpha1.AppStatus
		saveError      error
		storeHas       map[int64]bool
		wantError      bool
		wantSaved      bool
		wantState      string
	}{
		"downloads a new add-on": {
			wantSaved: true,
			wantState: stateDownloaded,
		},
		"skips an unchanged add-on": {
			existing: []licensingv1alpha1.AppStatus{
				{Checksum: "abc123", State: stateDownloaded, VirtualEntryID: 42},
			},
			storeHas:  map[int64]bool{42: true},
			wantSaved: false,
			wantState: stateDownloaded,
		},
		"re-downloads when the checksum changed": {
			existing: []licensingv1alpha1.AppStatus{
				{Checksum: "stale", State: stateDownloaded, VirtualEntryID: 42},
			},
			storeHas:  map[int64]bool{42: true},
			wantSaved: true,
			wantState: stateDownloaded,
		},
		"re-downloads when the file is missing": {
			existing: []licensingv1alpha1.AppStatus{
				{Checksum: "abc123", State: stateDownloaded, VirtualEntryID: 42},
			},
			storeHas:  map[int64]bool{42: false},
			wantSaved: true,
			wantState: stateDownloaded,
		},
		"records failed on a download error": {
			downloadErrors: map[int64]error{42: fmt.Errorf("boom")},
			wantSaved:      false,
			wantState:      stateFailed,
			wantError:      true,
		},
		"records failed when the store rejects the save": {
			saveError: fmt.Errorf("checksum mismatch"),
			wantSaved: false,
			wantState: stateFailed,
			wantError: true,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			downloader := &fakeDownloader{
				bodies: map[int64][]byte{42: []byte("lpkg")},
				errors: testCase.downloadErrors,
			}
			store := &fakeStore{
				has:       testCase.storeHas,
				saveError: testCase.saveError,
			}

			apps, error := Sync(
				[]provisioning.AddOn{addOn}, context.Background(), downloader,
				"env-1", testCase.existing, nil, store,
			)

			if testCase.wantError && (error == nil) {
				t.Fatal("Expected an error, got nil")
			}

			if !testCase.wantError && (error != nil) {
				t.Fatalf("Unexpected error: %v", error)
			}

			if len(apps) != 1 {
				t.Fatalf("Expected 1 app status, got %d", len(apps))
			}

			if apps[0].State != testCase.wantState {
				t.Errorf("State = %q, want %q", apps[0].State, testCase.wantState)
			}

			if apps[0].VirtualEntryID != 42 {
				t.Errorf("VirtualEntryID = %d, want 42", apps[0].VirtualEntryID)
			}

			_, saved := store.saved[42]

			if saved != testCase.wantSaved {
				t.Errorf("saved = %v, want %v", saved, testCase.wantSaved)
			}
		})
	}
}

type fakeDownloader struct {
	bodies map[int64][]byte
	errors map[int64]error
}

func (fakeDownloader *fakeDownloader) DownloadAddOn(
	context context.Context,
	downloadRequest provisioning.DownloadRequest,
	privateKey *rsa.PrivateKey,
) (io.ReadCloser, error) {
	if error := fakeDownloader.errors[downloadRequest.VirtualEntryID]; error != nil {
		return nil, error
	}

	return io.NopCloser(
		bytes.NewReader(fakeDownloader.bodies[downloadRequest.VirtualEntryID]),
	), nil
}

type fakeStore struct {
	has       map[int64]bool
	saveError error
	saved     map[int64][]byte
}

func (fakeStore *fakeStore) Has(
	checksum string, virtualEntryID int64,
) (bool, error) {
	return fakeStore.has[virtualEntryID], nil
}

func (fakeStore *fakeStore) Save(
	expectedChecksum string, reader io.Reader, virtualEntryID int64,
) error {
	if fakeStore.saveError != nil {
		return fakeStore.saveError
	}

	data, error := io.ReadAll(reader)

	if error != nil {
		return error
	}

	if fakeStore.saved == nil {
		fakeStore.saved = map[int64][]byte{}
	}

	fakeStore.saved[virtualEntryID] = data

	return nil
}
