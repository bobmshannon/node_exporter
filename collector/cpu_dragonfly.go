// Copyright 2015 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build !nocpu

package collector

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"
)

/*
#cgo LDFLAGS:
#include <sys/sysctl.h>
#include <kinfo.h>
#include <stdlib.h>
#include <stdio.h>

int
getCPUTimes(char **cputime) {
	size_t len;

	// Get number of cpu cores.
	int mib[2];
	int ncpu;
	mib[0] = CTL_HW;
	mib[1] = HW_NCPU;
	len = sizeof(ncpu);
	if (sysctl(mib, 2, &ncpu, &len, NULL, 0)) {
		return -1;
	}

	// The bump on each statclock is
	// ((cur_systimer - prev_systimer) * systimer_freq) >> 32
	// where
	// systimer_freq = sysctl kern.cputimer.freq
	long freq;
	len = sizeof(freq);
	if (sysctlbyname("kern.cputimer.freq", &freq, &len, NULL, 0)) {
		return -1;
	}

	// Get the cpu times.
	struct kinfo_cputime cp_t[ncpu];
	bzero(cp_t, sizeof(struct kinfo_cputime)*ncpu);
	len = sizeof(cp_t[0])*ncpu;
	if (sysctlbyname("kern.cputime", &cp_t, &len, NULL, 0)) {
		return -1;
	}

	// string needs to hold (5*ncpu)(uint64_t + char)
	// The char is the space between values.
	int cputime_size = (sizeof(uint64_t)+sizeof(char))*(5*ncpu);
	*cputime = (char *) malloc(cputime_size);
	bzero(*cputime, cputime_size);

	uint64_t user, nice, sys, intr, idle;
	user = nice = sys = intr = idle = 0;
	for (int i = 0; i < ncpu; ++i) {
		user = ((double) cp_t[i].cp_user) / freq;
		nice = ((double) cp_t[i].cp_nice) / freq;
		sys  = ((double) cp_t[i].cp_sys) / freq;
		intr = ((double) cp_t[i].cp_intr) / freq;
		idle = ((double) cp_t[i].cp_idle) / freq;
		sprintf(*cputime + strlen(*cputime), "%llu %llu %llu %llu %llu ", user, nice, sys, intr, idle );
	}

	return 0;

}
*/
import "C"

const maxCPUTimesLen = C.MAXCPU * C.CPUSTATES

type statCollector struct {
	cpu *prometheus.Desc
}

func init() {
	Factories["cpu"] = NewStatCollector
}

// Takes a prometheus registry and returns a new Collector exposing
// CPU stats.
func NewStatCollector() (Collector, error) {
	return &statCollector{
		cpu: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "", "cpu"),
			"Seconds the cpus spent in each mode.",
			[]string{"cpu", "mode"}, nil,
		),
	}, nil
}

// Expose CPU stats using sysctl.
func (c *statCollector) Update(ch chan<- prometheus.Metric) error {

	// We want time spent per-cpu per CPUSTATE.
	// CPUSTATES (number of CPUSTATES) is defined as 5U.
	// States: CP_USER | CP_NICE | CP_SYS | CP_IDLE | CP_INTR
	//
	// Each value is a counter incremented at frequency
	//   kern.cputimer.freq
	//
	// Look into sys/kern/kern_clock.c for details.

	var cpuTimesC *C.char
	var fieldsCount = 5

	if C.getCPUTimes(&cpuTimesC) == -1 {
		return errors.New("could not retrieve CPU times")
	}

	cpuTimes := strings.Split(strings.TrimSpace(C.GoString(cpuTimesC)), " ")
	C.free(unsafe.Pointer(cpuTimesC))

	// Export order: user nice sys intr idle
	cpuFields := []string{"user", "nice", "sys", "interrupt", "idle"}
	for i, v := range cpuTimes {
		cpux := fmt.Sprintf("cpu%d", i/fieldsCount)
		value, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return err
		}
		ch <- prometheus.MustNewConstMetric(c.cpu, prometheus.CounterValue, value, cpux, cpuFields[i%fieldsCount])
	}

	return nil
}
