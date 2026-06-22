// Package gradle implements Maven-layout mirror internals for JVM/Gradle ecosystems.
// Use pkg.MavenMirrorService and pkg.GradlePluginPortalMirrorService instead.
package gradle

import (
	"encoding/xml"
	"fmt"
	"strings"
)

type mavenMetadataXML struct {
	XMLName    xml.Name `xml:"metadata"`
	GroupId    string   `xml:"groupId"`
	ArtifactId string   `xml:"artifactId"`
	Versioning struct {
		Latest   string `xml:"latest"`
		Release  string `xml:"release"`
		Versions struct {
			Version []string `xml:"version"`
		} `xml:"versions"`
	} `xml:"versioning"`
}

type parsedMavenMetadata struct {
	GroupId    string
	ArtifactId string
	Latest     string
	Release    string
	Versions   []string
}

func mavenGroupPath(groupID string) string {
	return strings.ReplaceAll(groupID, ".", "/")
}

func mavenRepositoryBase(baseURL string) string {
	return strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
}

func mavenMetadataURL(baseURL, groupID, artifactID string) string {
	base := mavenRepositoryBase(baseURL)
	return fmt.Sprintf(
		"%s/%s/%s/maven-metadata.xml",
		base,
		mavenGroupPath(groupID),
		artifactID,
	)
}

func mavenArtifactURL(baseURL, groupID, artifactID, version, extension string) string {
	if extension == "" {
		extension = "jar"
	}

	base := mavenRepositoryBase(baseURL)
	fileName := fmt.Sprintf("%s-%s.%s", artifactID, version, extension)

	return fmt.Sprintf(
		"%s/%s/%s/%s/%s",
		base,
		mavenGroupPath(groupID),
		artifactID,
		version,
		fileName,
	)
}

func parseMavenCoordinates(
	packageName string,
	groupIDOverride, artifactIDOverride string,
) (groupID, artifactID string, err error) {
	if groupIDOverride != "" && artifactIDOverride != "" {
		return groupIDOverride, artifactIDOverride, nil
	}

	parts := strings.SplitN(strings.TrimSpace(packageName), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf(
			"package name must be groupId:artifactId (e.g. org.apache.commons:commons-lang3), got %q",
			packageName,
		)
	}

	return parts[0], parts[1], nil
}

func parseMavenMetadata(body []byte) (*parsedMavenMetadata, error) {
	var doc mavenMetadataXML
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}

	versions := doc.Versioning.Versions.Version
	if len(versions) == 0 && doc.Versioning.Latest != "" {
		versions = []string{doc.Versioning.Latest}
	}

	return &parsedMavenMetadata{
		GroupId:    doc.GroupId,
		ArtifactId: doc.ArtifactId,
		Latest:     doc.Versioning.Latest,
		Release:    doc.Versioning.Release,
		Versions:   versions,
	}, nil
}

func resolveMavenVersion(meta *parsedMavenMetadata, requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}

	if meta.Release != "" {
		return meta.Release, nil
	}

	if meta.Latest != "" {
		return meta.Latest, nil
	}

	if len(meta.Versions) > 0 {
		return meta.Versions[len(meta.Versions)-1], nil
	}

	return "", fmt.Errorf("no versions found in maven-metadata.xml")
}

func gradlePluginMarkerCoordinates(pluginID string) (groupID, artifactID string) {
	pluginID = strings.TrimSpace(pluginID)
	return pluginID, pluginID + ".gradle.plugin"
}

func gradlePluginPortalRoot(portalURL string) string {
	base := mavenRepositoryBase(portalURL)
	base = strings.TrimSuffix(base, "/m2")
	return strings.TrimSuffix(base, "/")
}

func gradlePluginMavenBase(portalURL string) string {
	return gradlePluginPortalRoot(portalURL) + "/m2"
}
