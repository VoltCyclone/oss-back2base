package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove the compose project's containers + built image",
	RunE:  runClean,
}

var wipeCmd = &cobra.Command{
	Use:   "wipe-images",
	Short: "Nuke ALL oss-back2base containers and images",
	RunE:  runWipe,
}

func init() {
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(wipeCmd)
}

func runClean(cmd *cobra.Command, args []string) error {
	s, err := ensureReady()
	if err != nil {
		return err
	}

	fmt.Println("Removing containers and images...")
	composeArgs := baseComposeArgs(s.cfg)
	composeArgs = append(composeArgs, "down", "--rmi", "local")
	exec.Command("docker", composeArgs...).Run() // best-effort

	fmt.Printf("Note: %s is NOT removed. To wipe state, rm -rf it manually.\n", s.cfg.ConfigDir)
	fmt.Println("Done.")
	return nil
}

func runWipe(cmd *cobra.Command, args []string) error {
	cfg := resolveConfig()

	// Find oss-back2base/back2base-compatible images.
	imgOut, _ := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	var imageRefs []string
	for _, line := range strings.Split(string(imgOut), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "oss-back2base-claude") ||
			strings.HasPrefix(line, "back2base-claude") ||
			strings.HasPrefix(line, "ramseymcgrath/back2base-base") ||
			strings.HasPrefix(line, "back2base/back2base:") {
			imageRefs = append(imageRefs, line)
		}
	}

	// Find oss-back2base/back2base-compatible containers from image names and
	// compose labels.
	ctrOut1, _ := exec.Command("docker", "ps", "-a",
		"--format", "{{.Names}}\t{{.Image}}").Output()
	ctrOut2a, _ := exec.Command("docker", "ps", "-a",
		"--filter", "label=com.docker.compose.project=back2base",
		"--format", "{{.Names}}").Output()
	ctrOut2b, _ := exec.Command("docker", "ps", "-a",
		"--filter", "label=com.docker.compose.project=oss-back2base",
		"--format", "{{.Names}}").Output()
	ctrOut2 := append(ctrOut2a, ctrOut2b...)

	containerSet := make(map[string]bool)
	for _, line := range strings.Split(string(ctrOut1), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) == 2 {
			img := parts[1]
			if strings.HasPrefix(img, "oss-back2base-claude") ||
				strings.HasPrefix(img, "back2base-claude") ||
				strings.HasPrefix(img, "back2base/back2base") {
				containerSet[parts[0]] = true
			}
		}
	}
	for _, line := range strings.Split(string(ctrOut2), "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			containerSet[name] = true
		}
	}

	containers := make([]string, 0, len(containerSet))
	for name := range containerSet {
		containers = append(containers, name)
	}

	if len(imageRefs) == 0 && len(containers) == 0 {
		fmt.Println("No oss-back2base containers or images found.")
		return nil
	}

	// Show what will be removed
	fmt.Println("This will permanently remove:")
	fmt.Println()
	if len(containers) > 0 {
		fmt.Println("Containers:")
		for _, name := range containers {
			out, _ := exec.Command("docker", "ps", "-a",
				"--filter", "name=^/"+name+"$",
				"--format", "  {{.Names}}  {{.Image}}  ({{.Status}})").Output()
			fmt.Print(string(out))
		}
		fmt.Println()
	}
	if len(imageRefs) > 0 {
		fmt.Println("Images:")
		for _, ref := range imageRefs {
			out, _ := exec.Command("docker", "images", ref,
				"--format", "  {{.Repository}}:{{.Tag}}  ({{.Size}})").Output()
			fmt.Print(string(out))
		}
		fmt.Println()
	}
	fmt.Printf("User state at %s is NOT touched.\n\n", cfg.ConfigDir)

	// Confirm unless --yes
	if !flagYes {
		fmt.Print("Proceed? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if len(containers) > 0 {
		fmt.Println(":: stopping and removing containers...")
		rmArgs := append([]string{"rm", "-f"}, containers...)
		exec.Command("docker", rmArgs...).Run()
	}
	if len(imageRefs) > 0 {
		fmt.Println(":: removing images...")
		rmiArgs := append([]string{"rmi", "-f"}, imageRefs...)
		exec.Command("docker", rmiArgs...).Run()
	}

	fmt.Println("Done. Next 'oss-back2base' run will rebuild from source.")
	return nil
}
