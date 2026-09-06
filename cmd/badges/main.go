package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"webtyp.com/devflow"
)

func main() {
	// Define flags
	_ = flag.String("module-name", "testmodule", "Module name")
	testStatus := flag.String("test-status", "Passing", "Test status")
	coveragePercent := flag.String("coverage", "85", "Coverage percentage")
	raceStatus := flag.String("race-status", "Clean", "Race status")
	vetStatus := flag.String("vet-status", "OK", "Vet status")
	licenseType := flag.String("license", "MIT", "License type")
	readmeFile := flag.String("readme", "README.md", "Readme file")

	flag.Parse()

	args := flag.Args()
	if len(args) == 1 && strings.HasSuffix(args[0], ".md") {
		*readmeFile = args[0]
	}

	// Get Go version
	g, gErr := devflow.NewGo(nil)
	var goVersion string
	if gErr != nil {
		devflow.PrintWarning(fmt.Sprintf("Could not initialize Go handler: %v. Defaulting to 1.22", gErr))
		goVersion = "1.22"
	} else {
		ver, err := g.GoVersion()
		if err != nil {
			devflow.PrintWarning(fmt.Sprintf("Could not get Go version: %v. Defaulting to 1.22", err))
			goVersion = "1.22"
		} else {
			goVersion = ver
		}
	}

	// Get badge colors
	licenseColor := getBadgeColor("license", *licenseType)
	goColor := getBadgeColor("go", goVersion)
	testColor := getBadgeColor("tests", *testStatus)
	coverageColor := getBadgeColor("coverage", *coveragePercent)
	raceColor := getBadgeColor("race", *raceStatus)
	vetColor := getBadgeColor("vet", *vetStatus)

	// Create badge strings
	badgeArgs := []string{
		fmt.Sprintf("readmefile:%s", *readmeFile),
		fmt.Sprintf("License:%s:%s", *licenseType, licenseColor),
		fmt.Sprintf("Go:%s:%s", goVersion, goColor),
		fmt.Sprintf("Tests:%s:%s", *testStatus, testColor),
		fmt.Sprintf("Coverage:%s%%:%s", *coveragePercent, coverageColor),
		fmt.Sprintf("Race:%s:%s", *raceStatus, raceColor),
		fmt.Sprintf("Vet:%s:%s", *vetStatus, vetColor),
	}

	// Create badge handler and build badges
	handler := devflow.NewBadges(badgeArgs...)
	if handler.Err() != nil {
		devflow.PrintError(handler.Err().Error())
		os.Exit(1)
	}

	_, bErr := handler.BuildBadges()
	if bErr != nil {
		devflow.PrintError(bErr.Error())
		os.Exit(1)
	}
}

// getBadgeColor determines the color for a badge based on its type and value.
func getBadgeColor(badgeType, value string) string {
	switch badgeType {
	case "license":
		return "#007acc"
	case "go":
		return "#00ADD8"
	case "tests":
		if value == "Passing" {
			return "#4c1"
		}
		return "#e05d44"
	case "coverage":
		var num int
		fmt.Sscanf(value, "%d", &num)
		if num >= 80 {
			return "#4c1"
		} else if num >= 60 {
			return "#dfb317"
		} else if num > 0 {
			return "#fe7d37"
		}
		return "#e05d44"
	case "race":
		if value == "Clean" {
			return "#4c1"
		}
		return "#e05d44"
	case "vet":
		if value == "OK" {
			return "#4c1"
		}
		return "#e05d44"
	default:
		return "#007acc"
	}
}
