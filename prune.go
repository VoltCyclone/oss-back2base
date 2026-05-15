package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var (
	pruneKeep   int
	pruneYes    bool
	pruneDryRun bool
	pruneQuiet  bool
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove old base/container images that no longer match this binary",
	Long: `Removes old ramseymcgrath/back2base-base:v* tags except the version
this binary pins (plus --keep N most-recent prior versions for rollback) and
orphan oss-back2base/back2base compose images not attached to any container. Then runs
'docker image prune -f' to free the dangling layers underneath.

Never touches :latest, never touches images attached to a running or stopped
container. Package post-install hooks and 'oss-back2base update' call this with
--yes --quiet automatically.`,
	RunE: runPrune,
}

func init() {
	pruneCmd.Flags().IntVar(&pruneKeep, "keep", 0,
		"Keep N most-recent prior versions in addition to the current pin")
	pruneCmd.Flags().BoolVar(&pruneYes, "yes", false, "Skip confirmation prompt")
	pruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false,
		"Show what would be removed without removing")
	pruneCmd.Flags().BoolVar(&pruneQuiet, "quiet", false,
		"Print only a one-line summary; implies --yes")
	rootCmd.AddCommand(pruneCmd)
}

// dockerImage is one row from `docker images`.
type dockerImage struct {
	repo string
	tag  string
	id   string
	size string
}

func (d dockerImage) ref() string {
	if d.tag == "" || d.tag == "<none>" {
		return d.id
	}
	return d.repo + ":" + d.tag
}

// listDockerImages enumerates all local images. Caller filters.
func listDockerImages() ([]dockerImage, error) {
	out, err := exec.Command("docker", "images",
		"--format", "{{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.Size}}").Output()
	if err != nil {
		return nil, err
	}
	return parseDockerImages(string(out)), nil
}

func parseDockerImages(raw string) []dockerImage {
	var rows []dockerImage
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		rows = append(rows, dockerImage{
			repo: parts[0], tag: parts[1], id: parts[2], size: parts[3],
		})
	}
	return rows
}

// imagesInUse returns image refs (or short IDs) attached to any container,
// running or stopped. `docker ps` exposes only `.Image`, which holds either
// the image's repo:tag or its short ID when no tag is attached. We index
// both as-is and as a normalized short ID for ID-keyed lookups.
func imagesInUse() (map[string]bool, error) {
	out, err := exec.Command("docker", "ps", "-a",
		"--format", "{{.Image}}").Output()
	if err != nil {
		return nil, err
	}
	return parseInUse(string(out)), nil
}

func parseInUse(raw string) map[string]bool {
	inUse := make(map[string]bool)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		inUse[line] = true
		// `.Image` may be a sha256:… ID when the image is untagged; also
		// index it by short form so callers can look up by 12-char prefix.
		inUse[shortID(line)] = true
	}
	return inUse
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// selectBaseVictims picks ramseymcgrath/back2base-base:v* tags to remove.
// Skips :latest, the current pin, and the top `keep` most-recent prior
// versions by semver. When currentTag is empty (dev build), returns nil —
// we don't know what to keep, so we keep everything.
func selectBaseVictims(images []dockerImage, currentTag string, keep int) []dockerImage {
	if currentTag == "" {
		return nil
	}
	var baseTags []dockerImage
	for _, img := range images {
		if img.repo != baseImageRepo {
			continue
		}
		if img.tag == "latest" || img.tag == currentTag || img.tag == "<none>" {
			continue
		}
		if !strings.HasPrefix(img.tag, "v") {
			continue // be conservative — only touch v-prefixed tags
		}
		baseTags = append(baseTags, img)
	}
	// Sort newest-first by semver so we can keep the top N.
	sort.Slice(baseTags, func(i, j int) bool {
		a := parseSemver(baseTags[i].tag)
		b := parseSemver(baseTags[j].tag)
		for k := 0; k < 3; k++ {
			if a[k] != b[k] {
				return a[k] > b[k]
			}
		}
		return baseTags[i].tag > baseTags[j].tag
	})
	if keep < 0 {
		keep = 0
	}
	if keep >= len(baseTags) {
		return nil
	}
	return baseTags[keep:]
}

// selectOrphanCompose picks compose-built and legacy images to remove.
//
// `back2base-claude:latest` is the compose project's active image (see
// back2base-container/docker-compose.yml — the "claude" service builds
// in-place, named after the project). We deliberately keep that tag,
// since in-use detection is unreliable right after a user stops a session.
// Other tags under `back2base-claude` or `oss-back2base-claude` (rare) get
// the standard in-use guard.
//
// `claudebox*` and `ramseymcgrath/claudebox*` are legacy artifacts from
// the back2base ← claudebox rename. They are never current; prune them
// outright unless still attached to a container.
//
// <none>-tagged dangling layers are skipped here — `docker image prune -f`
// afterward reclaims them.
func selectOrphanCompose(images []dockerImage, inUse map[string]bool) []dockerImage {
	var orphans []dockerImage
	for _, img := range images {
		if img.tag == "<none>" || img.repo == "<none>" {
			continue
		}
		switch {
		case img.repo == "back2base-claude", img.repo == "oss-back2base-claude":
			if img.tag == "latest" {
				continue // active build — leave alone
			}
		case strings.HasPrefix(img.repo, "back2base-claude-"),
			strings.HasPrefix(img.repo, "oss-back2base-claude-"):
			// Same family as above; same active-build rule.
			if img.tag == "latest" {
				continue
			}
		case strings.HasPrefix(img.repo, "claudebox-claude"),
			strings.HasPrefix(img.repo, "ramseymcgrath/claudebox"):
			// Legacy rename leftovers — always prunable.
		default:
			continue // unrelated image
		}
		if inUse[img.ref()] || inUse[img.id] || inUse[shortID(img.id)] {
			continue
		}
		orphans = append(orphans, img)
	}
	return orphans
}

func runPrune(cmd *cobra.Command, args []string) error {
	if pruneQuiet {
		pruneYes = true // quiet mode is non-interactive — implies --yes
	}

	// Use `docker info` (not `version`) — `version` succeeds with just the
	// CLI installed; `info` requires the daemon to be reachable.
	if err := exec.Command("docker", "info").Run(); err != nil {
		if !pruneQuiet {
			fmt.Fprintln(os.Stderr, ":: docker daemon not reachable; skipping prune")
		}
		return nil
	}

	// Below this point, any docker subcommand failure is treated as a
	// silent no-op — we never want a partial-outage to fail package-manager
	// post-install hooks or the auto-prune after `oss-back2base update`.
	images, err := listDockerImages()
	if err != nil {
		if !pruneQuiet {
			fmt.Fprintf(os.Stderr, ":: docker images failed (%v); skipping prune\n", err)
		}
		return nil
	}
	inUse, err := imagesInUse()
	if err != nil {
		if !pruneQuiet {
			fmt.Fprintf(os.Stderr, ":: docker ps failed (%v); skipping prune\n", err)
		}
		return nil
	}

	currentTag := baseImageTag
	if currentTag == "dev" {
		currentTag = "" // dev binary doesn't pin a version
	}

	baseVictims := selectBaseVictims(images, currentTag, pruneKeep)
	claudeVictims := selectOrphanCompose(images, inUse)

	if len(baseVictims) == 0 && len(claudeVictims) == 0 {
		if !pruneQuiet {
			fmt.Println("Nothing to prune.")
		}
		// Still run dangling-layer cleanup in non-quiet mode — there may be
		// orphaned layers from past rebuilds even when no tagged images match.
		if !pruneQuiet && !pruneDryRun {
			runDanglingPrune()
		}
		return nil
	}

	if !pruneQuiet {
		fmt.Println("This will remove:")
		for _, v := range baseVictims {
			fmt.Printf("  %s  (%s)\n", v.ref(), v.size)
		}
		for _, v := range claudeVictims {
			fmt.Printf("  %s  (%s)\n", v.ref(), v.size)
		}
		fmt.Println()
		fmt.Println("Plus dangling layers under those images.")
	}

	if pruneDryRun {
		if !pruneQuiet {
			fmt.Println("(dry-run) — not removing")
		}
		return nil
	}

	if !pruneYes {
		fmt.Print("Proceed? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	var refs []string
	for _, v := range baseVictims {
		refs = append(refs, v.ref())
	}
	for _, v := range claudeVictims {
		refs = append(refs, v.ref())
	}

	rmiArgs := append([]string{"rmi"}, refs...)
	rmiCmd := exec.Command("docker", rmiArgs...)
	if !pruneQuiet {
		rmiCmd.Stdout = os.Stdout
		rmiCmd.Stderr = os.Stderr
	}
	_ = rmiCmd.Run() // best-effort; some refs may already be gone

	runDanglingPrune()

	total := len(baseVictims) + len(claudeVictims)
	if pruneQuiet {
		if total > 0 {
			fmt.Printf(":: pruned %d old back2base image(s)\n", total)
		}
	} else {
		fmt.Println("Done.")
	}
	return nil
}

// runDanglingPrune removes layers no longer referenced by any tag. After
// our targeted rmi calls, the freed layers fall into "dangling" status;
// this is what actually reclaims disk space.
func runDanglingPrune() {
	cmd := exec.Command("docker", "image", "prune", "-f")
	if !pruneQuiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	_ = cmd.Run()
}
