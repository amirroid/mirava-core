package gradle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MiravaOrg/mirava-core/internal/constants"
	"github.com/MiravaOrg/mirava-core/pkg/apt"
)

type MavenMirrorService struct {
	HttpClient *http.Client
}

type MavenCheckSpeedParams struct {
	GroupId    string
	ArtifactId string
	Version    string
	Extension  string
}

type MavenCheckSpeedData struct {
	DownloadMb      float64
	DurationSec     float64
	ContentLength   int64
	TimeoutSec      int
	SpeedMBps       float64
	SpeedRating     string
	BytesDownloaded int64
	GroupId         string
	ArtifactId      string
	Version         string
	TestURL         string
	MirrorURL       string
}

type MavenCheckPackageParams struct {
	GroupId    string
	ArtifactId string
}

type MavenCheckPackageData struct {
	GroupId       string
	ArtifactId    string
	Version       string
	Latest        string
	Release       string
	VersionsCount int
	AllVersions   []string
	MetadataURL   string
}

type MavenCheckStatusData struct {
	Status      bool
	TestPath    string
	StatusCode  int
	GroupId     string
	ArtifactId  string
	MetadataURL string
}

const (
	defaultMavenSpeedGroupId     = "org.apache.commons"
	defaultMavenSpeedArtifactId  = "commons-lang3"
	defaultMavenStatusGroupId    = "org.apache.commons"
	defaultMavenStatusArtifactId = "commons-lang3"
)

func (m *MavenMirrorService) fetchMavenMetadata(
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

func (m *MavenMirrorService) httpClient() *http.Client {
	if m.HttpClient != nil {
		return m.HttpClient
	}

	return &http.Client{Timeout: 60 * time.Second}
}

func (m *MavenMirrorService) CheckSpeed(
	mirrorURL string,
	timeout int,
	verbose bool,
	params MavenCheckSpeedParams,
) (float64, *MavenCheckSpeedData, error) {
	baseURL := mavenRepositoryBase(mirrorURL)

	groupID := params.GroupId
	if groupID == "" {
		groupID = defaultMavenSpeedGroupId
	}

	artifactID := params.ArtifactId
	if artifactID == "" {
		artifactID = defaultMavenSpeedArtifactId
	}

	extension := params.Extension
	if extension == "" {
		extension = "jar"
	}

	version := params.Version
	if version == "" {
		meta, _, err := m.fetchMavenMetadata(baseURL, groupID, artifactID)
		if err != nil {
			return 0, nil, err
		}

		version, err = resolveMavenVersion(meta, "")
		if err != nil {
			return 0, nil, &apt.HttpRequestError{
				URL: mavenMetadataURL(baseURL, groupID, artifactID),
				Err: err,
			}
		}
	}

	testURL := mavenArtifactURL(baseURL, groupID, artifactID, version, extension)

	if verbose {
		fmt.Printf(
			"Testing Maven mirror speed with: %s (timeout: %d seconds)\n",
			testURL,
			timeout,
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
			fmt.Printf("Artifact size: %.2f MB\n", float64(contentLength)/1024/1024)
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

		info := MavenCheckSpeedData{
			DownloadMb:      float64(downloaded) / 1024 / 1024,
			DurationSec:     duration,
			ContentLength:   contentLength,
			TimeoutSec:      timeout,
			SpeedMBps:       speedMBps,
			SpeedRating:     speedRating(speedMBps),
			BytesDownloaded: downloaded,
			GroupId:         groupID,
			ArtifactId:      artifactID,
			Version:         version,
			TestURL:         testURL,
			MirrorURL:       baseURL,
		}

		return speedMBps, &info, nil
	}

	return 0, nil, &apt.SpeedTestError{
		URL: testURL,
		Err: fmt.Errorf("speed test failed (downloaded %d bytes in %.2fs)", downloaded, duration),
	}
}

func (m *MavenMirrorService) CheckPackage(
	mirrorURL string,
	packageName string,
	verbose bool,
	params MavenCheckPackageParams,
) (bool, *MavenCheckPackageData, error) {
	baseURL := mavenRepositoryBase(mirrorURL)

	groupID, artifactID, err := parseMavenCoordinates(
		packageName,
		params.GroupId,
		params.ArtifactId,
	)
	if err != nil {
		return false, nil, err
	}

	meta, metaURL, err := m.fetchMavenMetadata(baseURL, groupID, artifactID)
	if err != nil {
		return false, nil, err
	}

	version, err := resolveMavenVersion(meta, "")
	if err != nil {
		return false, nil, &apt.HttpRequestError{URL: metaURL, Err: err}
	}

	if verbose {
		fmt.Printf(
			"Found artifact '%s:%s' with latest version: %s (%d versions in metadata)\n",
			groupID,
			artifactID,
			version,
			len(meta.Versions),
		)
	}

	info := &MavenCheckPackageData{
		GroupId:       groupID,
		ArtifactId:    artifactID,
		Version:       version,
		Latest:        meta.Latest,
		Release:       meta.Release,
		VersionsCount: len(meta.Versions),
		AllVersions:   meta.Versions,
		MetadataURL:   metaURL,
	}

	return true, info, nil
}

func (m *MavenMirrorService) CheckStatus(
	url string,
	verbose bool,
) (bool, *MavenCheckStatusData, error) {
	baseURL := mavenRepositoryBase(url)

	candidates := []struct {
		groupID    string
		artifactID string
	}{
		{defaultMavenStatusGroupId, defaultMavenStatusArtifactId},
		{"junit", "junit"},
	}

	for _, c := range candidates {
		metaURL := mavenMetadataURL(baseURL, c.groupID, c.artifactID)

		if verbose {
			fmt.Printf("Testing Maven mirror endpoint: %s\n", metaURL)
		}

		req, err := http.NewRequest(http.MethodGet, metaURL, nil)
		if err != nil {
			continue
		}

		req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		req.Header.Set("Accept", "application/xml, text/xml, */*")
		req.Header.Set("User-Agent", constants.UserAgent)

		resp, err := m.httpClient().Do(req)
		if err != nil {
			if verbose {
				fmt.Printf("Error checking endpoint: %v\n", err)
			}

			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK || readErr != nil {
			continue
		}

		if _, err := parseMavenMetadata(body); err != nil {
			continue
		}

		if verbose {
			fmt.Printf("Mirror responded to metadata with status %d\n", resp.StatusCode)
		}

		info := MavenCheckStatusData{
			Status:      true,
			TestPath:    strings.TrimPrefix(metaURL, baseURL),
			StatusCode:  resp.StatusCode,
			GroupId:     c.groupID,
			ArtifactId:  c.artifactID,
			MetadataURL: metaURL,
		}

		return true, &info, nil
	}

	return false, nil, &apt.InvalidMirrorError{URL: url}
}

func NewMavenMirrorService() *MavenMirrorService {
	return &MavenMirrorService{
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
