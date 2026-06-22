package gradle

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MiravaOrg/mirava-core/pkg/apt"
)

const sampleGradleMarkerMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>org.gradle.wrapper</groupId>
  <artifactId>org.gradle.wrapper.gradle.plugin</artifactId>
  <versioning>
    <latest>1.0.0</latest>
    <release>1.0.0</release>
    <versions>
      <version>1.0.0</version>
    </versions>
  </versioning>
</metadata>`

const sampleGradleMarkerPOM = `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <dependencies>
    <dependency>
      <groupId>org.gradle</groupId>
      <artifactId>gradle-wrapper</artifactId>
      <version>1.0.0</version>
    </dependency>
  </dependencies>
</project>`

func newGradlePluginPortalTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/m2/org/gradle/wrapper/org.gradle.wrapper.gradle.plugin/maven-metadata.xml":
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(sampleGradleMarkerMetadata))
		case "/m2/org/gradle/wrapper/org.gradle.wrapper.gradle.plugin/1.0.0/org.gradle.wrapper.gradle.plugin-1.0.0.pom":
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(sampleGradleMarkerPOM))
		case "/m2/org/gradle/gradle-wrapper/1.0.0/gradle-wrapper-1.0.0.jar":
			w.Write(make([]byte, 128*1024))
		case "/api/plugins/org.gradle.wrapper":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"org.gradle.wrapper","version":"1.0.0","versions":[{"version":"1.0.0"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestGradlePluginPortalCheckStatusViaMetadata(t *testing.T) {
	server := newGradlePluginPortalTestServer(t)
	defer server.Close()

	service := NewGradlePluginPortalMirrorService()

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
	if info.UsedPortalAPI {
		t.Fatal("expected metadata-based status check")
	}
}

func TestGradlePluginPortalCheckPackage(t *testing.T) {
	server := newGradlePluginPortalTestServer(t)
	defer server.Close()

	service := NewGradlePluginPortalMirrorService()

	ok, info, err := service.CheckPackage(server.URL, "org.gradle.wrapper", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected plugin to exist")
	}
	if info.Version != "1.0.0" {
		t.Fatalf("unexpected version %q", info.Version)
	}
	if info.MarkerArtifactId != "org.gradle.wrapper.gradle.plugin" {
		t.Fatalf("unexpected marker artifact %q", info.MarkerArtifactId)
	}
}

func TestGradlePluginPortalCheckPackageNotFound(t *testing.T) {
	server := newGradlePluginPortalTestServer(t)
	defer server.Close()

	service := NewGradlePluginPortalMirrorService()

	ok, info, err := service.CheckPackage(server.URL, "com.example.missing", false)
	if err == nil {
		t.Fatal("expected error for missing plugin")
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

func TestGradlePluginPortalCheckSpeed(t *testing.T) {
	server := newGradlePluginPortalTestServer(t)
	defer server.Close()

	service := NewGradlePluginPortalMirrorService()

	speed, info, err := service.CheckSpeed(
		server.URL,
		5,
		false,
		GradlePluginCheckSpeedParams{
			PluginId: "org.gradle.wrapper",
			Version:  "1.0.0",
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if speed <= 0 {
		t.Fatalf("expected positive speed, got %f", speed)
	}
	if info == nil || !strings.Contains(info.TestURL, "gradle-wrapper-1.0.0.jar") {
		t.Fatalf("unexpected test URL: %+v", info)
	}
	if info.ImplementationGroupId != "org.gradle" {
		t.Fatalf("unexpected implementation group %q", info.ImplementationGroupId)
	}
	if info.SpeedRating != "Excellent" {
		t.Fatalf("expected Excellent rating for fast local download, got %q", info.SpeedRating)
	}
}

func TestGradlePluginMarkerCoordinates(t *testing.T) {
	group, artifact := gradlePluginMarkerCoordinates("org.springframework.boot")
	if group != "org.springframework.boot" {
		t.Fatalf("unexpected group %q", group)
	}
	if artifact != "org.springframework.boot.gradle.plugin" {
		t.Fatalf("unexpected artifact %q", artifact)
	}
}

func TestGradlePluginMavenBase(t *testing.T) {
	if got := gradlePluginMavenBase("https://plugins.gradle.org"); got != "https://plugins.gradle.org/m2" {
		t.Fatalf("unexpected m2 base %q", got)
	}
	if got := gradlePluginMavenBase("https://mirror.example/plugins/m2"); got != "https://mirror.example/plugins/m2" {
		t.Fatalf("unexpected m2 base %q", got)
	}
}
