// Copyright 2023 The Parca Authors
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

package convert

import (
	"io"
	"strconv"
	"strings"

	"github.com/google/pprof/profile"
	"github.com/pyroscope-io/jfr-parser/parser"
)

type builder struct {
	profile       *profile.Profile
	locationTable map[string]*profile.Location
	functionTable map[string]*profile.Function
	sampleTable   map[string]*profile.Sample
}

func newBuilder() *builder {
	return &builder{
		profile:       &profile.Profile{SampleType: []*profile.ValueType{{Type: "cpu", Unit: "samples"}}},
		locationTable: map[string]*profile.Location{},
		functionTable: map[string]*profile.Function{},
		sampleTable:   map[string]*profile.Sample{},
	}
}

func JfrToPprof(r io.Reader) (*profile.Profile, error) {
	chunks, err := parser.Parse(r)
	if err != nil {
		return nil, err
	}

	b := newBuilder()
	for _, c := range chunks {
		b.addJFRChunk(c)
	}

	return b.profile, nil
}

func (b *builder) addJFRChunk(c parser.Chunk) {
	var event string
	for _, e := range c.Events {
		if as, ok := e.(*parser.ActiveSetting); ok {
			// Extract the event name from the active setting.
			if as.Name == "event" {
				event = as.Value
			}
		}
	}
	if event != "cpu" {
		return
	}

	for _, event := range extractExecutionSampleEvents(c.Events) {
		if event.State.Name == "STATE_RUNNABLE" {
			increaseSample(b.getOrCreateSample(event.StackTrace))
		}
	}
}

func increaseSample(s *profile.Sample) {
	if s == nil {
		return
	}

	s.Value[0]++
}

func (b *builder) getOrCreateSample(st *parser.StackTrace) *profile.Sample {
	if st == nil {
		return nil
	}

	locations := make([]*profile.Location, 0, len(st.Frames))
	locationKeys := make([]string, 0, len(st.Frames))
	for i := len(st.Frames) - 1; i >= 0; i-- {
		f := st.Frames[i]
		if f.Method != nil && f.Method.Type != nil && f.Method.Type.Name != nil && f.Method.Name != nil {
			fun := b.getOrCreateFunction(f.Method.Type.Name.String + "." + f.Method.Name.String)
			locKey, loc := b.getOrCreateLocation(fun, f.LineNumber)
			locations = append(locations, loc)
			locationKeys = append(locationKeys, locKey)
		}
	}

	sampleKey := strings.Join(locationKeys, ";")
	s, ok := b.sampleTable[sampleKey]
	if !ok {
		s = &profile.Sample{
			Location: locations,
			Value:    []int64{0},
		}

		b.sampleTable[sampleKey] = s
		b.profile.Sample = append(b.profile.Sample, s)
	}

	return s
}

func (b *builder) getOrCreateFunction(name string) *profile.Function {
	if f, ok := b.functionTable[name]; ok {
		return f
	}

	f := &profile.Function{
		ID:   uint64(len(b.functionTable) + 1),
		Name: name,
	}
	b.functionTable[name] = f
	b.profile.Function = append(b.profile.Function, f)
	return f
}

func (b *builder) getOrCreateLocation(fun *profile.Function, line int32) (string, *profile.Location) {
	line64 := int64(line)
	key := fun.Name + ":" + strconv.FormatInt(line64, 10)
	if l, ok := b.locationTable[key]; ok {
		return key, l
	}

	l := &profile.Location{
		ID:      uint64(len(b.locationTable) + 1),
		Line:    []profile.Line{{Function: fun, Line: line64}},
		Address: uint64(line),
	}
	b.locationTable[key] = l
	b.profile.Location = append(b.profile.Location, l)
	return key, l
}

func extractExecutionSampleEvents(events []parser.Parseable) []*parser.ExecutionSample {
	res := []*parser.ExecutionSample{}
	for _, e := range events {
		// There are a lot of events that we don't care about. We only care about on-CPU samples.
		if es, ok := e.(*parser.ExecutionSample); ok {
			res = append(res, es)
		}
	}
	return res
}
