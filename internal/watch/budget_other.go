// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

//go:build !darwin && !linux

package watch

// Reserve accepts any plan.
//
// Windows watches a directory through ReadDirectoryChangesW, which holds a
// handle and an overlapped read per directory and has no small documented
// ceiling to check against; handles are bounded by memory rather than by a
// quota. The remaining platforms use kqueue or FEN without a limit this can
// read. In every case a refusal here would be invented rather than measured,
// and fsnotify reports the real failure if one comes.
func Reserve(Plan) error { return nil }
