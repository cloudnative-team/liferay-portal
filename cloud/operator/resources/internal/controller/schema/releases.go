package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (httpReleaseCatalog *HTTPReleaseCatalog) GetSchemaVersion(
	context context.Context,
	image string,
) (string, error) {
	releaseKey, error := getReleaseKey(image)

	if error != nil {
		return "", error
	}

	schemaVersions, error := httpReleaseCatalog.getSchemaVersions(context)

	if error != nil {
		return "", error
	}

	schemaVersion, ok := schemaVersions[releaseKey]

	if !ok {
		return "", fmt.Errorf("release %s is not in %s", releaseKey, httpReleaseCatalog.URL)
	}

	return schemaVersion, nil
}

// compareVersions compares dotted numeric versions such as 38.2.12.
func compareVersions(version1 string, version2 string) (int, error) {
	parts1 := strings.Split(version1, ".")
	parts2 := strings.Split(version2, ".")

	for index := 0; index < max(len(parts1), len(parts2)); index++ {
		number1, error := getVersionPart(index, parts1)

		if error != nil {
			return 0, fmt.Errorf("invalid version %q: %w", version1, error)
		}

		number2, error := getVersionPart(index, parts2)

		if error != nil {
			return 0, fmt.Errorf("invalid version %q: %w", version2, error)
		}

		if number1 != number2 {
			if number1 < number2 {
				return -1, nil
			}

			return 1, nil
		}
	}

	return 0, nil
}

// getReleaseKey maps an official image such as liferay/dxp:2026.q1.14-lts
// to its releases.json key dxp-2026.q1.14-lts.
func getReleaseKey(image string) (string, error) {
	index := strings.LastIndex(image, ":")

	if (index < 0) || strings.Contains(image[index:], "/") {
		return "", fmt.Errorf("image %s has no tag", image)
	}

	repository := image[:index]

	if (repository != "liferay/dxp") && !strings.HasSuffix(repository, "/liferay/dxp") {
		return "", fmt.Errorf("image %s is not an official liferay/dxp image", image)
	}

	tag := strings.ToLower(image[index+1:])

	tag = strings.TrimSuffix(tag, "-slim")

	return "dxp-" + tag, nil
}

func (httpReleaseCatalog *HTTPReleaseCatalog) getSchemaVersions(
	context context.Context,
) (map[string]string, error) {
	httpReleaseCatalog.mutex.Lock()

	defer httpReleaseCatalog.mutex.Unlock()

	if (httpReleaseCatalog.schemaVersions != nil) &&
		(time.Since(httpReleaseCatalog.fetchedAt) < httpReleaseCatalog.CacheDuration) {

		return httpReleaseCatalog.schemaVersions, nil
	}

	request, error := http.NewRequestWithContext(
		context, http.MethodGet, httpReleaseCatalog.URL, nil,
	)

	if error != nil {
		return nil, error
	}

	httpClient := httpReleaseCatalog.HTTPClient

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	response, error := httpClient.Do(request)

	if error != nil {
		return nil, error
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"unable to read %s: HTTP %d", httpReleaseCatalog.URL,
			response.StatusCode,
		)
	}

	var releases []struct {
		DatabaseSchemaVersion string `json:"databaseSchemaVersion"`
		ReleaseKey            string `json:"releaseKey"`
	}

	if error := json.NewDecoder(response.Body).Decode(&releases); error != nil {
		return nil, fmt.Errorf("unable to parse %s: %w", httpReleaseCatalog.URL, error)
	}

	schemaVersions := map[string]string{}

	for _, release := range releases {
		if release.DatabaseSchemaVersion != "" {
			schemaVersions[release.ReleaseKey] = release.DatabaseSchemaVersion
		}
	}

	httpReleaseCatalog.fetchedAt = time.Now()
	httpReleaseCatalog.schemaVersions = schemaVersions

	return schemaVersions, nil
}

func getVersionPart(index int, parts []string) (int, error) {
	if index >= len(parts) {
		return 0, nil
	}

	return strconv.Atoi(parts[index])
}

// HTTPReleaseCatalog reads the database schema version of each DXP release
// from Liferay's releases.json.
type HTTPReleaseCatalog struct {
	CacheDuration time.Duration
	HTTPClient    *http.Client
	URL           string

	fetchedAt      time.Time
	mutex          sync.Mutex
	schemaVersions map[string]string
}
