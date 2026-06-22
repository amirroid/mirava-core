package gradle

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MiravaOrg/mirava-core/internal/constants"
	"github.com/MiravaOrg/mirava-core/pkg/apt"
)

type GradlePluginPortalMirrorService struct {
	HttpClient *http.Client
}

type GradlePluginCheckSpeedParams struct {
	PluginId string
	Version  string
}

type GradlePluginCheckSpeedData struct {
	DownloadMb               float64
	DurationSec              float64
	ContentLength            int64
	TimeoutSec               int
	SpeedMBps                float64
	SpeedRating              string
	BytesDownloaded          int64
	PluginId                 string
	Version                  string
	MarkerGroupId            string
	MarkerArtifactId         string
	ImplementationGroupId    string
	ImplementationArtifactId string
	TestURL                  string
	MirrorURL                string
}

type GradlePluginCheckPackageData struct {
	PluginId         string
	Version          string
	Latest           string
	Release          string
	VersionsCount    int
	AllVersions      []string
	MarkerGroupId    string
	MarkerArtifactId string
	MetadataURL      string
	PortalAPIURL     string
}

type GradlePluginCheckStatusData struct {
	Status           bool
	TestPath         string
	StatusCode       int
	PluginId         string
	MarkerGroupId    string
	MarkerArtifactId string
	MetadataURL      string
	UsedPortalAPI    bool
}

type gradlePluginPortalAPIResponse struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Website  string `json:"website"`
	Versions []struct {
		Version string `json:"version"`
	} `json:"versions"`
}

type gradlePluginMarkerPOM struct {
	Dependencies []struct {
		GroupId    string `xml:"groupId"`
		ArtifactId string `xml:"artifactId"`
		Version    string `xml:"version"`
	} `xml:"dependencies>dependency"`
}

const (
	defaultGradlePluginID       = "org.gradle.wrapper"
	defaultGradleStatusPluginID = "org.gradle.wrapper"
)

func (m *GradlePluginPortalMirrorService) httpClient() *http.Client {
	if m.HttpClient != nil {
		return m.HttpClient
	}

	return &http.Client{Timeout: 60 * time.Second}
}

func (m *GradlePluginPortalMirrorService) fetchMarkerMetadata(
	m2Base, pluginID string,
) (*parsedMavenMetadata, string, error) {
	groupID, artifactID := gradlePluginMarkerCoordinates(pluginID)
	return m.fetchMavenMetadataViaClient(m2Base, groupID, artifactID)
}

func (m *GradlePluginPortalMirrorService) fetchMavenMetadataViaClient(
	baseURL, groupID, artifactID string,
) (*parsedMavenMetadata, string, error) {
	metaURL := mavenMetadataURL(baseURL, groupID, artifactID)

	req, err := http.NewRequest(http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, metaURL, &apt.HttpRequestError{URL: metaURL, Err: err}
	}

	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	req.Header.Set("Accept", "application/xml, text/xml, */*")
	req.Header.Set("User-Agent", constants.UserAgent)

	resp, err := m.httpClient().Do(req)
	if err != nil {
		return nil, metaURL, &apt.HttpRequestError{URL: metaURL, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, metaURL, &apt.PackageNotFoundError{Package: groupID + ":" + artifactID}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, metaURL, &apt.HttpRequestError{
			URL:        metaURL,
			StatusCode: resp.StatusCode,
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, metaURL, &apt.ResponseReadError{URL: metaURL, Err: err}
	}

	meta, err := parseMavenMetadata(body)
	if err != nil {
		return nil, metaURL, &apt.JsonParseError{URL: metaURL, Err: err}
	}

	return meta, metaURL, nil
}

func (m *GradlePluginPortalMirrorService) resolveImplementationArtifact(
	m2Base, pluginID, version string,
) (groupID, artifactID, artifactURL string, err error) {
	markerGroup, markerArtifact := gradlePluginMarkerCoordinates(pluginID)
	pomURL := mavenArtifactURL(m2Base, markerGroup, markerArtifact, version, "pom")

	req, err := http.NewRequest(http.MethodGet, pomURL, nil)
	if err != nil {
		return "", "", "", &apt.HttpRequestError{URL: pomURL, Err: err}
	}

	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	req.Header.Set("Accept", "application/xml, text/xml, */*")
	req.Header.Set("User-Agent", constants.UserAgent)

	resp, err := m.httpClient().Do(req)
	if err != nil {
		return "", "", "", &apt.HttpRequestError{URL: pomURL, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", "", &apt.HttpRequestError{
			URL:        pomURL,
			StatusCode: resp.StatusCode,
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", "", "", &apt.ResponseReadError{URL: pomURL, Err: err}
	}

	var pom gradlePluginMarkerPOM
	if err := xml.Unmarshal(body, &pom); err != nil {
		return "", "", "", &apt.JsonParseError{URL: pomURL, Err: err}
	}

	for _, dep := range pom.Dependencies {
		if dep.GroupId == "" || dep.ArtifactId == "" {
			continue
		}

		if dep.GroupId == markerGroup && dep.ArtifactId == markerArtifact {
			continue
		}

		depVersion := dep.Version
		if depVersion == "" {
			depVersion = version
		}

		groupID = dep.GroupId
		artifactID = dep.ArtifactId
		artifactURL = mavenArtifactURL(m2Base, groupID, artifactID, depVersion, "jar")

		return groupID, artifactID, artifactURL, nil
	}

	return "", "", "", fmt.Errorf(
		"plugin marker POM at %s does not declare an implementation dependency",
		pomURL,
	)
}

func (m *GradlePluginPortalMirrorService) CheckSpeed(
	mirrorURL string,
	timeout int,
	verbose bool,
	params GradlePluginCheckSpeedParams,
) (float64, *GradlePluginCheckSpeedData, error) {
	m2Base := gradlePluginMavenBase(mirrorURL)

	pluginID := params.PluginId
	if pluginID == "" {
		pluginID = defaultGradlePluginID
	}

	version := params.Version
	if version == "" {
		meta, _, err := m.fetchMarkerMetadata(m2Base, pluginID)
		if err != nil {
			return 0, nil, err
		}

		version, err = resolveMavenVersion(meta, "")
		if err != nil {
			return 0, nil, err
		}
	}

	implGroup, implArtifact, testURL, err := m.resolveImplementationArtifact(m2Base, pluginID, version)
	if err != nil {
		return 0, nil, err
	}

	markerGroup, markerArtifact := gradlePluginMarkerCoordinates(pluginID)

	if verbose {
		fmt.Printf(
			"Testing Gradle Plugin Portal mirror speed with: %s (timeout: %d seconds)\n",
			testURL,
			timeout,
		)
		fmt.Printf(
			"Plugin %s@%s -> %s:%s\n",
			pluginID,
			version,
			implGroup,
			implArtifact,
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return 0, nil, &apt.HttpRequestError{URL: testURL, Err: err}
	}

	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	req.Header.Set("User-Agent", constants.UserAgent)

	start := time.Now()

	resp, err := m.httpClient().Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return 0, nil, &apt.TimeoutError{
				URL:     testURL,
				Timeout: timeout,
				Err:     ctx.Err(),
			}
		}

		return 0, nil, &apt.HttpRequestError{URL: testURL, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, nil, &apt.HttpRequestError{
			URL:        testURL,
			StatusCode: resp.StatusCode,
		}
	}

	contentLength := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 512*1024)
	lastProgress := time.Now()

	if verbose {
		if contentLength > 0 {
			fmt.Printf("Plugin JAR size: %.2f MB\n", float64(contentLength)/1024/1024)
		}

		fmt.Printf("Downloading for up to %d seconds...\n", timeout)
	}

downloadLoop:
	for {
		select {
		case <-ctx.Done():
			goto calculateSpeed
		default:
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				downloaded += int64(n)

				if verbose && time.Since(lastProgress) > 500*time.Millisecond {
					elapsed := time.Since(start).Seconds()
					speedMBps := (float64(downloaded) / 1024 / 1024) / elapsed

					if contentLength > 0 {
						percent := float64(downloaded) / float64(contentLength) * 100
						fmt.Printf(
							"\r[%ds] %.1f%% (%.2f/%.2f MB) - %.2f MB/s",
							int(elapsed),
							percent,
							float64(downloaded)/1024/1024,
							float64(contentLength)/1024/1024,
							speedMBps,
						)
					} else {
						fmt.Printf(
							"\r[%ds] Downloaded: %.2f MB - %.2f MB/s",
							int(elapsed),
							float64(downloaded)/1024/1024,
							speedMBps,
						)
					}

					lastProgress = time.Now()
				}
			}

			if readErr != nil {
				if readErr == io.EOF {
					if verbose {
						fmt.Println()
					}

					break downloadLoop
				}

				if ctx.Err() == context.DeadlineExceeded {
					break downloadLoop
				}

				return 0, nil, &apt.HttpRequestError{URL: testURL, Err: readErr}
			}
		}
	}

calculateSpeed:
	duration := time.Since(start).Seconds()

	if verbose {
		fmt.Printf(
			"\nDownloaded %.2f MB in %.2f seconds\n",
			float64(downloaded)/1024/1024,
			duration,
		)
	}

	if duration > 0 && downloaded > 0 {
		speedMBps := (float64(downloaded) / 1024 / 1024) / duration

		if verbose {
			fmt.Printf("Average speed: %.2f MB/s\n", speedMBps)
			fmt.Printf("Rating: %s\n", speedRating(speedMBps))
		}

		info := GradlePluginCheckSpeedData{
			DownloadMb:               float64(downloaded) / 1024 / 1024,
			DurationSec:              duration,
			ContentLength:            contentLength,
			TimeoutSec:               timeout,
			SpeedMBps:                speedMBps,
			SpeedRating:              speedRating(speedMBps),
			BytesDownloaded:          downloaded,
			PluginId:                 pluginID,
			Version:                  version,
			MarkerGroupId:            markerGroup,
			MarkerArtifactId:         markerArtifact,
			ImplementationGroupId:    implGroup,
			ImplementationArtifactId: implArtifact,
			TestURL:                  testURL,
			MirrorURL:                gradlePluginPortalRoot(mirrorURL),
		}

		return speedMBps, &info, nil
	}

	return 0, nil, &apt.SpeedTestError{
		URL: testURL,
		Err: fmt.Errorf("speed test failed (downloaded %d bytes in %.2fs)", downloaded, duration),
	}
}

func (m *GradlePluginPortalMirrorService) CheckPackage(
	mirrorURL string,
	pluginID string,
	verbose bool,
) (bool, *GradlePluginCheckPackageData, error) {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return false, nil, fmt.Errorf("plugin id is empty")
	}

	m2Base := gradlePluginMavenBase(mirrorURL)
	markerGroup, markerArtifact := gradlePluginMarkerCoordinates(pluginID)

	meta, metaURL, err := m.fetchMarkerMetadata(m2Base, pluginID)
	if err != nil {
		return false, nil, err
	}

	version, err := resolveMavenVersion(meta, "")
	if err != nil {
		return false, nil, &apt.HttpRequestError{URL: metaURL, Err: err}
	}

	portalRoot := gradlePluginPortalRoot(mirrorURL)
	apiURL := portalRoot + "/api/plugins/" + pluginID

	if verbose {
		fmt.Printf(
			"Found plugin '%s' with latest version: %s (%d versions in marker metadata)\n",
			pluginID,
			version,
			len(meta.Versions),
		)
	}

	info := &GradlePluginCheckPackageData{
		PluginId:         pluginID,
		Version:          version,
		Latest:           meta.Latest,
		Release:          meta.Release,
		VersionsCount:    len(meta.Versions),
		AllVersions:      meta.Versions,
		MarkerGroupId:    markerGroup,
		MarkerArtifactId: markerArtifact,
		MetadataURL:      metaURL,
		PortalAPIURL:     apiURL,
	}

	return true, info, nil
}

func (m *GradlePluginPortalMirrorService) tryPortalAPIStatus(
	portalRoot, pluginID string,
	verbose bool,
) (bool, *GradlePluginCheckStatusData, error) {
	apiURL := portalRoot + "/api/plugins/" + pluginID

	if verbose {
		fmt.Printf("Testing Gradle Plugin Portal API endpoint: %s\n", apiURL)
	}

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return false, nil, err
	}

	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", constants.UserAgent)

	resp, err := m.httpClient().Do(req)
	if err != nil {
		return false, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, nil, fmt.Errorf("portal API returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return false, nil, err
	}

	var api gradlePluginPortalAPIResponse
	if err := json.Unmarshal(body, &api); err != nil {
		return false, nil, err
	}

	if api.ID == "" {
		return false, nil, fmt.Errorf("portal API response missing plugin id")
	}

	markerGroup, markerArtifact := gradlePluginMarkerCoordinates(pluginID)

	info := &GradlePluginCheckStatusData{
		Status:           true,
		TestPath:         "/api/plugins/" + pluginID,
		StatusCode:       resp.StatusCode,
		PluginId:         pluginID,
		MarkerGroupId:    markerGroup,
		MarkerArtifactId: markerArtifact,
		UsedPortalAPI:    true,
	}

	return true, info, nil
}

func (m *GradlePluginPortalMirrorService) CheckStatus(
	url string,
	verbose bool,
) (bool, *GradlePluginCheckStatusData, error) {
	portalRoot := gradlePluginPortalRoot(url)
	m2Base := gradlePluginMavenBase(url)

	pluginID := defaultGradleStatusPluginID
	markerGroup, markerArtifact := gradlePluginMarkerCoordinates(pluginID)
	metaURL := mavenMetadataURL(m2Base, markerGroup, markerArtifact)

	if verbose {
		fmt.Printf("Testing Gradle Plugin Portal marker metadata: %s\n", metaURL)
	}

	req, err := http.NewRequest(http.MethodGet, metaURL, nil)
	if err == nil {
		req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		req.Header.Set("Accept", "application/xml, text/xml, */*")
		req.Header.Set("User-Agent", constants.UserAgent)

		resp, doErr := m.httpClient().Do(req)
		if doErr == nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK && readErr == nil {
				if _, parseErr := parseMavenMetadata(body); parseErr == nil {
					if verbose {
						fmt.Printf(
							"Mirror responded to marker metadata with status %d\n",
							resp.StatusCode,
						)
					}

					info := GradlePluginCheckStatusData{
						Status:           true,
						TestPath:         strings.TrimPrefix(metaURL, m2Base),
						StatusCode:       resp.StatusCode,
						PluginId:         pluginID,
						MarkerGroupId:    markerGroup,
						MarkerArtifactId: markerArtifact,
						MetadataURL:      metaURL,
						UsedPortalAPI:    false,
					}

					return true, &info, nil
				}
			}
		}
	}

	ok, info, err := m.tryPortalAPIStatus(portalRoot, pluginID, verbose)
	if ok {
		return true, info, nil
	}

	if verbose && err != nil {
		fmt.Printf("Portal API check failed: %v\n", err)
	}

	return false, nil, &apt.InvalidMirrorError{URL: url}
}

func NewGradlePluginPortalMirrorService() *GradlePluginPortalMirrorService {
	return &GradlePluginPortalMirrorService{
		HttpClient: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				DisableCompression:  false,
				DisableKeepAlives:   false,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}
