package cli

import (
	"strconv"
	"strings"
	"time"
)

// latestSceneSkillVersions keeps the response shape and all metadata, reducing
// only each skill's versions array. The backend's ordering is not assumed.
func latestSceneSkillVersions(data interface{}) interface{} {
	skills, ok := data.([]interface{})
	if !ok {
		return data
	}
	for _, item := range skills {
		skill, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		versions, ok := skill["versions"].([]interface{})
		if !ok || len(versions) <= 1 {
			continue
		}
		var latest map[string]interface{}
		for _, item := range versions {
			version, ok := item.(map[string]interface{})
			if ok && (latest == nil || sceneSkillVersionNewer(version, latest)) {
				latest = version
			}
		}
		if latest != nil {
			skill["versions"] = []interface{}{latest}
		}
	}
	return data
}

func sceneSkillVersionNewer(candidate, current map[string]interface{}) bool {
	candidateDate := sceneSkillUploadDate(candidate["uploadDate"])
	currentDate := sceneSkillUploadDate(current["uploadDate"])
	if !candidateDate.Equal(currentDate) {
		return candidateDate.After(currentDate)
	}

	// For equal/missing timestamps, compare dotted numeric versions numerically
	// (0.2.10 > 0.2.9). Keep backend order for ties or unrecognized version labels.
	candidateVersion, candidateOK := sceneSkillVersionParts(candidate["version"])
	currentVersion, currentOK := sceneSkillVersionParts(current["version"])
	if !candidateOK || !currentOK {
		return false
	}
	for i := 0; i < len(candidateVersion) || i < len(currentVersion); i++ {
		var a, b uint64
		if i < len(candidateVersion) {
			a = candidateVersion[i]
		}
		if i < len(currentVersion) {
			b = currentVersion[i]
		}
		if a != b {
			return a > b
		}
	}
	return false
}

func sceneSkillUploadDate(value interface{}) time.Time {
	raw, _ := value.(string)
	// The service's unzoned uploadDate is a China Standard Time wall clock.
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, strings.TrimSpace(raw), time.FixedZone("CST", 8*60*60)); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func sceneSkillVersionParts(value interface{}) ([]uint64, bool) {
	raw, _ := value.(string)
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "v")
	parts := strings.Split(raw, ".")
	numbers := make([]uint64, len(parts))
	for i, part := range parts {
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return nil, false
		}
		numbers[i] = number
	}
	return numbers, true
}
