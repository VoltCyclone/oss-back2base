package main

import (
	"reflect"
	"sort"
	"testing"
)

func TestParseDockerImages(t *testing.T) {
	raw := "ramseymcgrath/back2base-base\tv0.19.10\tabc123\t512MB\n" +
		"ramseymcgrath/back2base-base\tv0.19.23\tdef456\t548MB\n" +
		"ramseymcgrath/back2base-base\tlatest\tdef456\t548MB\n" +
		"back2base-claude\tlatest\tffe999\t2.1GB\n" +
		"\n" + // blank line
		"<none>\t<none>\t777aaa\t312MB\n"
	got := parseDockerImages(raw)
	if len(got) != 5 {
		t.Fatalf("expected 5 rows, got %d: %+v", len(got), got)
	}
	if got[0].repo != "ramseymcgrath/back2base-base" || got[0].tag != "v0.19.10" {
		t.Errorf("row 0 wrong: %+v", got[0])
	}
	if got[4].repo != "<none>" || got[4].id != "777aaa" {
		t.Errorf("row 4 wrong: %+v", got[4])
	}
}

func TestParseInUse(t *testing.T) {
	// docker ps -a --format "{{.Image}}" output: one image ref per line.
	raw := "ramseymcgrath/back2base-base:v0.19.23\n" +
		"back2base-claude:latest\n" +
		"sha256:ffe999000aaa\n" + // untagged, shows as ID
		"\n"
	inUse := parseInUse(raw)

	cases := []string{
		"ramseymcgrath/back2base-base:v0.19.23",
		"back2base-claude:latest",
		"sha256:ffe999000aaa",
		"ffe999000aaa", // short-ID indexed too
	}
	for _, want := range cases {
		if !inUse[want] {
			t.Errorf("expected %q in inUse map, missing", want)
		}
	}
	if inUse["ramseymcgrath/back2base-base:v0.19.10"] {
		t.Errorf("v0.19.10 not in containers, should not be marked in-use")
	}
}

func TestSelectBaseVictimsKeepZero(t *testing.T) {
	images := []dockerImage{
		{repo: baseImageRepo, tag: "v0.19.10", id: "a", size: "512MB"},
		{repo: baseImageRepo, tag: "v0.19.20", id: "b", size: "520MB"},
		{repo: baseImageRepo, tag: "v0.19.23", id: "c", size: "548MB"},
		{repo: baseImageRepo, tag: "latest", id: "c", size: "548MB"},
		{repo: "unrelated/image", tag: "v0.19.10", id: "z", size: "100MB"},
	}
	got := selectBaseVictims(images, "v0.19.23", 0)

	gotTags := tagsOf(got)
	sort.Strings(gotTags)
	want := []string{"v0.19.10", "v0.19.20"}
	if !reflect.DeepEqual(gotTags, want) {
		t.Errorf("keep=0: got %v want %v", gotTags, want)
	}
}

func TestSelectBaseVictimsKeepOne(t *testing.T) {
	images := []dockerImage{
		{repo: baseImageRepo, tag: "v0.19.5", id: "a"},
		{repo: baseImageRepo, tag: "v0.19.10", id: "b"},
		{repo: baseImageRepo, tag: "v0.19.20", id: "c"},
		{repo: baseImageRepo, tag: "v0.19.23", id: "d"},
	}
	// Current is v0.19.23; keep=1 should retain the most-recent prior
	// (v0.19.20) and prune the older two.
	got := selectBaseVictims(images, "v0.19.23", 1)
	gotTags := tagsOf(got)
	sort.Strings(gotTags)
	want := []string{"v0.19.10", "v0.19.5"}
	if !reflect.DeepEqual(gotTags, want) {
		t.Errorf("keep=1: got %v want %v", gotTags, want)
	}
}

func TestSelectBaseVictimsSemverNotLexical(t *testing.T) {
	// Lexical sort would order v0.19.10 < v0.19.9, which is wrong.
	images := []dockerImage{
		{repo: baseImageRepo, tag: "v0.19.9", id: "a"},
		{repo: baseImageRepo, tag: "v0.19.10", id: "b"},
		{repo: baseImageRepo, tag: "v0.19.23", id: "c"},
	}
	// keep=1 should retain v0.19.10 (the most-recent prior to v0.19.23 by
	// semver), not v0.19.9 (which lexically sorts after v0.19.10).
	got := selectBaseVictims(images, "v0.19.23", 1)
	gotTags := tagsOf(got)
	if len(gotTags) != 1 || gotTags[0] != "v0.19.9" {
		t.Errorf("keep=1 with semver: expected only v0.19.9 to be victim, got %v", gotTags)
	}
}

func TestSelectBaseVictimsSkipsLatest(t *testing.T) {
	images := []dockerImage{
		{repo: baseImageRepo, tag: "latest", id: "a"},
		{repo: baseImageRepo, tag: "v0.19.23", id: "b"},
	}
	got := selectBaseVictims(images, "v0.19.23", 0)
	if len(got) != 0 {
		t.Errorf(":latest must never be a victim: got %+v", got)
	}
}

func TestSelectBaseVictimsSkipsCurrent(t *testing.T) {
	images := []dockerImage{
		{repo: baseImageRepo, tag: "v0.19.23", id: "a"},
	}
	got := selectBaseVictims(images, "v0.19.23", 0)
	if len(got) != 0 {
		t.Errorf("current pin must never be a victim: got %+v", got)
	}
}

func TestSelectBaseVictimsSkipsNonV(t *testing.T) {
	images := []dockerImage{
		{repo: baseImageRepo, tag: "experimental", id: "a"},
		{repo: baseImageRepo, tag: "v0.19.23", id: "b"},
	}
	got := selectBaseVictims(images, "v0.19.23", 0)
	if len(got) != 0 {
		t.Errorf("non-v* tags must be left alone: got %+v", got)
	}
}

func TestSelectBaseVictimsDevModeKeepsEverything(t *testing.T) {
	images := []dockerImage{
		{repo: baseImageRepo, tag: "v0.19.10", id: "a"},
		{repo: baseImageRepo, tag: "v0.19.23", id: "b"},
	}
	// Empty currentTag means dev build — never know what's "current",
	// so keep everything.
	got := selectBaseVictims(images, "", 0)
	if got != nil {
		t.Errorf("dev mode should produce no victims: got %+v", got)
	}
}

func TestSelectOrphanComposeKeepsActiveBuild(t *testing.T) {
	images := []dockerImage{
		{repo: "back2base-claude", tag: "latest", id: "a"},     // current build — KEEP
		{repo: "back2base-claude", tag: "v0.18.0", id: "b"},    // old version tag — prune if unused
		{repo: "back2base-claude", tag: "<none>", id: "c"},     // dangling — skipped (image prune -f handles)
	}
	got := selectOrphanCompose(images, nil)
	gotRefs := make([]string, len(got))
	for i, v := range got {
		gotRefs[i] = v.ref()
	}
	sort.Strings(gotRefs)
	want := []string{"back2base-claude:v0.18.0"}
	if !reflect.DeepEqual(gotRefs, want) {
		t.Errorf("got %v want %v", gotRefs, want)
	}
}

func TestSelectOrphanComposePrunesLegacyClaudebox(t *testing.T) {
	images := []dockerImage{
		{repo: "claudebox-claude", tag: "latest", id: "a"},
		{repo: "ramseymcgrath/claudebox-base", tag: "latest", id: "b"},
		{repo: "ramseymcgrath/claudebox-base", tag: "v0.10.0", id: "c"},
		{repo: "ramseymcgrath/claudebox", tag: "latest", id: "d"},
		{repo: "back2base-claude", tag: "latest", id: "e"}, // active — kept
	}
	got := selectOrphanCompose(images, nil)
	gotRefs := make([]string, len(got))
	for i, v := range got {
		gotRefs[i] = v.ref()
	}
	sort.Strings(gotRefs)
	want := []string{
		"claudebox-claude:latest",
		"ramseymcgrath/claudebox-base:latest",
		"ramseymcgrath/claudebox-base:v0.10.0",
		"ramseymcgrath/claudebox:latest",
	}
	if !reflect.DeepEqual(gotRefs, want) {
		t.Errorf("got %v want %v", gotRefs, want)
	}
}

func TestSelectOrphanComposeRespectsInUse(t *testing.T) {
	images := []dockerImage{
		{repo: "claudebox-claude", tag: "latest", id: "a"}, // legacy but in use → keep
	}
	inUse := map[string]bool{"claudebox-claude:latest": true}
	got := selectOrphanCompose(images, inUse)
	if len(got) != 0 {
		t.Errorf("in-use legacy image must be kept: got %+v", got)
	}
}

func TestSelectOrphanComposeIgnoresUnrelated(t *testing.T) {
	images := []dockerImage{
		{repo: "redis", tag: "7", id: "a"},
		{repo: "ramseymcgrath/back2base-base", tag: "v0.19.23", id: "b"}, // base, handled separately
	}
	got := selectOrphanCompose(images, nil)
	if len(got) != 0 {
		t.Errorf("only back2base-claude*/claudebox* repos should be candidates: got %+v", got)
	}
}

func TestShortID(t *testing.T) {
	cases := map[string]string{
		"sha256:abc123def456gggg": "abc123def456",
		"abc123def456gggg":        "abc123def456",
		"abc123":                  "abc123",
	}
	for in, want := range cases {
		if got := shortID(in); got != want {
			t.Errorf("shortID(%q) = %q, want %q", in, got, want)
		}
	}
}

// tagsOf is a small helper for the table tests above.
func tagsOf(imgs []dockerImage) []string {
	out := make([]string, len(imgs))
	for i, v := range imgs {
		out[i] = v.tag
	}
	return out
}
