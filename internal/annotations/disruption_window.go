/*
Copyright 2024 The Spotalis Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package annotations

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// DisruptionWindow defines a time window when pod disruptions are allowed
type DisruptionWindow struct {
	// Schedule is the parsed cron schedule
	Schedule cron.Schedule
	// Duration is how long the window lasts
	Duration time.Duration
}

// ParseDisruptionWindow parses disruption window annotations
// Both schedule and duration must be present together, or both absent.
func ParseDisruptionWindow(annotations map[string]string) (*DisruptionWindow, error) {
	scheduleStr := annotations[DisruptionScheduleAnnotation]
	durationStr := annotations[DisruptionDurationAnnotation]

	// Both must be present or both absent (XOR check)
	if (scheduleStr == "") != (durationStr == "") {
		return nil, fmt.Errorf("both %s and %s must be specified together",
			DisruptionScheduleAnnotation, DisruptionDurationAnnotation)
	}

	// No window configured
	if scheduleStr == "" {
		return nil, nil
	}

	// Parse cron schedule (5-field format: minute hour day month weekday)
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(scheduleStr)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule %q: %w", scheduleStr, err)
	}

	// Parse duration
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return nil, fmt.Errorf("invalid duration %q: %w", durationStr, err)
	}

	return &DisruptionWindow{
		Schedule: schedule,
		Duration: duration,
	}, nil
}

// IsWithinWindow checks if the given time is within the disruption window
func (dw *DisruptionWindow) IsWithinWindow(t time.Time) bool {
	// No window configured = always allowed
	if dw == nil {
		return true
	}

	// Convert to UTC for consistent cron evaluation
	t = t.UTC()

	// Find the last time the schedule triggered before current time
	// We do this by going back by the duration and asking "when is the next occurrence after that?"
	// This gives us the most recent schedule occurrence
	lastSchedule := dw.Schedule.Next(t.Add(-dw.Duration))

	// Check if we're within the window
	// Window is [lastSchedule, lastSchedule + duration)
	windowEnd := lastSchedule.Add(dw.Duration)

	return !t.Before(lastSchedule) && t.Before(windowEnd)
}
