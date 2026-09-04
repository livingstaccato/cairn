// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// Command csvheader prints the Go emitter's CSV column contract, so the
// end-to-end gate can compare it against what the Hugo template produces.
// Nothing else in the build compares the two.
package main

import (
	"fmt"
	"strings"

	"github.com/livingstaccato/cairn/internal/emit"
)

func main() {
	fmt.Println(strings.Join(emit.CSVHeader, ","))
}
