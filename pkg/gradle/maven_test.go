package gradle

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MiravaOrg/mirava-core/pkg/apt"
)

const sampleMavenMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>org.apache.commons</groupId>
  <artifactId>commons-lang3</artifactId>
  <versioning>
    <latest>3.14.0</latest>
    <release>3.14.0</release>
    <versions>
      <version>3.12.0</version>
      <version>3.14.0</version>
    </versions>
  </versioning>
</metadata>`

func newMavenTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/org/apache/commons/commons-lang3/maven-metadata.xml":
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(sampleMavenMetadata))
		case "/org/apache/commons/commons-lang3/3.14.0/commons-lang3-3.14.0.jar":
			w.Write(make([]byte, 256*1024))
		case "/junit/junit/maven-metadata.xml":
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(`<?xml version="1.0"?><metadata><groupId>junit</groupId><artifactId>junit</artifactId><versioning><latest>4.13.2</latest><release>4.13.2</release><versions><version>4.13.2</version></versions></versioning></metadata>`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestMavenCheckStatus(t *testing.T) {
	server := newMavenTestServer(t)
	defer server.Close()

	service := NewMavenMirrorService()

	ok, info, err := service.CheckStatus(server.URL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected mirror to be valid")
	}
	if info == nil || info.MetadataURL == "" {
		t.Fatal("expected metadata URL in status info")
	}
}

func TestMavenCheckPackage(t *testing.T) {
	server := newMavenTestServer(t)
	defer server.Close()

	service := NewMavenMirrorService()

	ok, info, err := service.CheckPackage(
		server.URL,
		"org.apache.commons:commons-lang3",
		false,
		MavenCheckPackageParams{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected artifact to exist")
	}
	if info.Version != "3.14.0" {
		t.Fatalf("unexpected version %q", info.Version)
	}
	if info.VersionsCount != 2 {
		t.Fatalf("expected 2 versions, got %d", info.VersionsCount)
	}
}

func TestMavenCheckPackageNotFound(t *testing.T) {
	server := newMavenTestServer(t)
	defer server.Close()

	service := NewMavenMirrorService()

	ok, info, err := service.CheckPackage(
		server.URL,
		"com.example:missing",
		false,
		MavenCheckPackageParams{},
	)
	if err == nil {
		t.Fatal("expected error for missing artifact")
	}
	if _, okErr := err.(*apt.PackageNotFoundError); !okErr {
		t.Fatalf("expected PackageNotFoundError, got %T: %v", err, err)
	}
	if ok {
		t.Fatal("expected ok=false")
	}
	if info != nil {
		t.Fatal("expected nil info on not found")
	}
}

func TestMavenCheckSpeed(t *testing.T) {
	server := newMavenTestServer(t)
	defer server.Close()

	service := NewMavenMirrorService()

	speed, info, err := service.CheckSpeed(
		server.URL,
		5,
		false,
		MavenCheckSpeedParams{
			GroupId:    "org.apache.commons",
			ArtifactId: "commons-lang3",
			Version:    "3.14.0",
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if speed <= 0 {
		t.Fatalf("expected positive speed, got %f", speed)
	}
	if info == nil || !strings.Contains(info.TestURL, "commons-lang3-3.14.0.jar") {
		t.Fatalf("unexpected test URL: %+v", info)
	}
	if info.SpeedRating == "" {
		t.Fatal("expected speed rating")
	}
}

func TestParseMavenCoordinates(t *testing.T) {
	group, artifact, err := parseMavenCoordinates("org.apache.commons:commons-lang3", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if group != "org.apache.commons" || artifact != "commons-lang3" {
		t.Fatalf("unexpected coordinates %s:%s", group, artifact)
	}

	_, _, err = parseMavenCoordinates("invalid", "", "")
	if err == nil {
		t.Fatal("expected error for invalid coordinates")
	}
}

func TestSpeedRatingMatchesPkgThresholds(t *testing.T) {
	cases := []struct {
		speed    float64
		expected string
	}{
		{25, "Excellent"},
		{15, "Good"},
		{7, "Average"},
		{3, "Slow"},
	}

	for _, tc := range cases {
		if got := speedRating(tc.speed); got != tc.expected {
			t.Fatalf("speedRating(%v) = %q, want %q", tc.speed, got, tc.expected)
		}
	}
}
