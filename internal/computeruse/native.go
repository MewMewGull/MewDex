package computeruse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"

	"mewdex/internal/childprocess"
	"mewdex/internal/protocol"
)

type nativeDriver struct{ program string }

type nativeRequest struct {
	Operation   string                       `json:"operation"`
	Window      protocol.ComputerWindow      `json:"window"`
	Observation protocol.ComputerObservation `json:"observation"`
	Bounds      Bounds                       `json:"bounds"`
	Action      protocol.ComputerAction      `json:"action"`
}

type nativeResult struct {
	Windows     []protocol.ComputerWindow     `json:"windows,omitempty"`
	Observation *protocol.ComputerObservation `json:"observation,omitempty"`
	Bounds      Bounds                        `json:"bounds"`
	Error       string                        `json:"error,omitempty"`
}

func NewNativeDriver(program string) Driver { return &nativeDriver{program: program} }

func (d *nativeDriver) call(ctx context.Context, request nativeRequest) (nativeResult, error) {
	cmd := exec.CommandContext(ctx, d.program, "computer-native")
	cmd.Stdin = bytes.NewReader(protocol.JSON(request))
	raw, err := childprocess.OutputContext(ctx, cmd, 2<<20)
	var result nativeResult
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, errors.New("invalid native computer response")
	}
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	return result, nil
}

func (d *nativeDriver) Windows(ctx context.Context) ([]protocol.ComputerWindow, error) {
	result, err := d.call(ctx, nativeRequest{Operation: "windows"})
	return result.Windows, err
}

func (d *nativeDriver) Observe(ctx context.Context, window protocol.ComputerWindow) (protocol.ComputerObservation, Bounds, error) {
	result, err := d.call(ctx, nativeRequest{Operation: "observe", Window: window})
	if err != nil {
		return protocol.ComputerObservation{}, Bounds{}, err
	}
	if result.Observation == nil || result.Observation.Width <= 0 || result.Observation.Height <= 0 || len(result.Observation.Image) > 1500000 || result.Observation.MimeType != "image/jpeg" {
		return protocol.ComputerObservation{}, Bounds{}, errors.New("native observation is invalid or exceeds its image limit")
	}
	return *result.Observation, result.Bounds, nil
}

func (d *nativeDriver) Act(ctx context.Context, window protocol.ComputerWindow, observation protocol.ComputerObservation, bounds Bounds, action protocol.ComputerAction) error {
	observation.Image = ""
	_, err := d.call(ctx, nativeRequest{Operation: "act", Window: window, Observation: observation, Bounds: bounds, Action: action})
	return err
}

// ServeNative is a one-shot child-process entry point. The node retains the
// desktop reservation and kills this owned worker when a request is cancelled.
// No network listener or model credential is involved.
func ServeNative(input io.Reader, output io.Writer) error {
	var request nativeRequest
	decoder := json.NewDecoder(io.LimitReader(input, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("one bounded native computer request required")
	}
	restore, err := prepareNative()
	if err != nil {
		return err
	}
	defer restore()
	var result nativeResult
	switch request.Operation {
	case "windows":
		result.Windows, err = nativeWindows()
	case "observe":
		var observation protocol.ComputerObservation
		observation, result.Bounds, err = nativeObserve(request.Window)
		result.Observation = &observation
	case "act":
		if err = validateAction(request.Action, request.Observation); err == nil {
			err = nativeAct(request)
		}
	default:
		err = errors.New("unsupported native computer operation")
	}
	if err != nil {
		result.Error = err.Error()
	}
	return json.NewEncoder(output).Encode(result)
}
