package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gptscript-ai/gptscript/pkg/runner"
	"github.com/gptscript-ai/gptscript/pkg/types"
)

type Options struct {
	DisplayProgress bool   `usage:"-"`
	DumpState       string `usage:"Dump the internal execution state to a file"`
}

func complete(opts ...Options) (result Options) {
	for _, opt := range opts {
		result.DumpState = types.FirstSet(opt.DumpState, result.DumpState)
		result.DisplayProgress = types.FirstSet(opt.DisplayProgress, result.DisplayProgress)
	}
	return
}

type Console struct {
	dumpState       string
	displayProgress bool
}

var (
	runID           int64
	prettyIDCounter int64
)

func (c *Console) Start(ctx context.Context, prg *types.Program, env []string, input string) (runner.Monitor, error) {
	id := atomic.AddInt64(&runID, 1)
	mon := newDisplay(c.dumpState, c.displayProgress)
	mon.dump.ID = fmt.Sprint(id)
	mon.dump.Program = prg
	mon.dump.Input = input

	log.Fields("runID", mon.dump.ID, "input", input, "program", prg).Debugf("Run started")
	return mon, nil
}

type display struct {
	dump        dump
	livePrinter *livePrinter
	dumpState   string
	callIDMap   map[string]string
	callLock    sync.Mutex
}

type livePrinter struct {
	lastLines    map[string]string
	needsNewline bool
}

func (l *livePrinter) end() {
	if l == nil {
		return
	}
	if l.needsNewline {
		fmt.Println()
	}
	l.needsNewline = false
}

func (l *livePrinter) print(event runner.Event, c call) {
	if l == nil {
		return
	}
	if c.ParentID != "" {
		return
	}

	last := l.lastLines[c.ID]
	line := strings.TrimPrefix(event.Content, last)
	fmt.Print(line)
	l.needsNewline = !strings.HasSuffix(line, "\n")
	l.lastLines[c.ID] = event.Content
}

func (d *display) Event(event runner.Event) {
	d.callLock.Lock()
	defer d.callLock.Unlock()

	var (
		currentIndex = -1
		currentCall  call
	)

	for i, existing := range d.dump.Calls {
		if event.CallContext.ID == existing.ID {
			currentIndex = i
			currentCall = existing
			break
		}
	}

	if currentIndex == -1 {
		currentIndex = len(d.dump.Calls)
		currentCall = call{
			ID:       event.CallContext.ID,
			ParentID: event.CallContext.ParentID(),
			ToolID:   event.CallContext.Tool.ID,
		}
		d.dump.Calls = append(d.dump.Calls, currentCall)
	}

	log := log.Fields(
		"id", currentCall.ID,
		"parentID", currentCall.ParentID,
		"toolID", currentCall.ToolID)

	prettyID, ok := d.callIDMap[currentCall.ID]
	if !ok {
		prettyID = fmt.Sprint(atomic.AddInt64(&prettyIDCounter, 1))
		d.callIDMap[currentCall.ID] = prettyID
	}

	callName := callName{
		prettyID: prettyID,
		call:     &currentCall,
		prg:      d.dump.Program,
		calls:    d.dump.Calls,
	}

	switch event.Type {
	case runner.EventTypeCallStart:
		d.livePrinter.end()
		currentCall.Start = event.Time
		currentCall.Input = event.Content
		log.Fields("input", event.Content).Infof("started  [%s]", callName)
	case runner.EventTypeCallProgress:
		d.livePrinter.print(event, currentCall)
	case runner.EventTypeCallContinue:
		d.livePrinter.end()
		log.Fields("toolResults", event.ToolResults).Infof("continue [%s]", callName)
	case runner.EventTypeChat:
		d.livePrinter.end()
		if event.ChatRequest == nil {
			log = log.Fields(
				"completionID", event.ChatCompletionID,
				"response", toJSON(event.ChatResponse),
				"cached", event.ChatResponseCached,
			)
		} else {
			log.Infof("sent     [%s]", callName)
			log = log.Fields(
				"completionID", event.ChatCompletionID,
				"request", toJSON(event.ChatRequest),
			)
		}
		log.Debugf("messages")
		currentCall.Messages = append(currentCall.Messages, message{
			CompletionID: event.ChatCompletionID,
			Request:      event.ChatRequest,
			Response:     event.ChatResponse,
			Cached:       event.ChatResponseCached,
		})
	case runner.EventTypeCallFinish:
		d.livePrinter.end()
		currentCall.End = event.Time
		currentCall.Output = event.Content
		log.Fields("output", event.Content).Infof("ended    [%s]", callName)
	}

	d.dump.Calls[currentIndex] = currentCall
}

func (d *display) Stop(output string, err error) {
	log.Fields("runID", d.dump.ID, "output", output, "err", err).Debugf("Run stopped")
	d.dump.Output = output
	d.dump.Err = err
	if d.dumpState != "" {
		f, err := os.Create(d.dumpState)
		if err == nil {
			_ = d.Dump(f)
			_ = f.Close()
		}
	}
}

func NewConsole(opts ...Options) *Console {
	opt := complete(opts...)
	return &Console{
		dumpState:       opt.DumpState,
		displayProgress: opt.DisplayProgress,
	}
}

func newDisplay(dumpState string, progress bool) *display {
	display := &display{
		dumpState: dumpState,
		callIDMap: make(map[string]string),
	}
	if progress {
		display.livePrinter = &livePrinter{
			lastLines: map[string]string{},
		}
	}
	return display
}

func (d *display) Dump(out io.Writer) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(d.dump)
}

func toJSON(obj any) jsonDump {
	return jsonDump{obj: obj}
}

type jsonDump struct {
	obj any
}

func (j jsonDump) MarshalJSON() ([]byte, error) {
	return json.Marshal(j.obj)
}

func (j jsonDump) String() string {
	d, err := json.Marshal(j.obj)
	if err != nil {
		return err.Error()
	}
	return string(d)
}

type callName struct {
	prettyID string
	call     *call
	prg      *types.Program
	calls    []call
}

func (c callName) String() string {
	var (
		msg         []string
		currentCall = c.call
	)

	for {
		tool := c.prg.ToolSet[currentCall.ToolID]
		name := tool.Name
		if name == "" {
			name = tool.Source.File
		}
		if currentCall.ID != "1" {
			name += "(" + c.prettyID + ")"
		}
		msg = append(msg, name)
		found := false
		for _, parent := range c.calls {
			if parent.ID == currentCall.ParentID {
				found = true
				currentCall = &parent
				break
			}
		}
		if !found {
			break
		}
	}

	slices.Reverse(msg)
	result := strings.Join(msg[1:], "->")
	if result == "" {
		return "main"
	}
	return result
}

type dump struct {
	ID      string         `json:"id,omitempty"`
	Program *types.Program `json:"program,omitempty"`
	Calls   []call         `json:"calls,omitempty"`
	Input   string         `json:"input,omitempty"`
	Output  string         `json:"output,omitempty"`
	Err     error          `json:"err,omitempty"`
}

type message struct {
	CompletionID string `json:"completionID,omitempty"`
	Request      any    `json:"request,omitempty"`
	Response     any    `json:"response,omitempty"`
	Cached       bool   `json:"cached,omitempty"`
}

type call struct {
	ID       string    `json:"id,omitempty"`
	ParentID string    `json:"parentID,omitempty"`
	ToolID   string    `json:"toolID,omitempty"`
	Messages []message `json:"messages,omitempty"`
	Start    time.Time `json:"start,omitempty"`
	End      time.Time `json:"end,omitempty"`
	Input    string    `json:"input,omitempty"`
	Output   string    `json:"output,omitempty"`
}

func (c call) String() string {
	return c.ID
}
