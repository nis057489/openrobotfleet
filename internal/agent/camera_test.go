package agent

import "testing"

func TestSetCameraResolutionYAML(t *testing.T) {
	got, err := setCameraResolutionYAML(cameraROSParams, 1280, 960)
	if err != nil {
		t.Fatal(err)
	}
	want := `/**:
  ros__parameters:
    format: RGB888
    width: 1280
    height: 960
    frame_id: ` + cameraFrameID + `
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	// A hand-edited file missing width/height gets them added under
	// ros__parameters with matching indentation.
	got, err = setCameraResolutionYAML("/**:\n  ros__parameters:\n    format: RGB888\n", 640, 480)
	if err != nil {
		t.Fatal(err)
	}
	want = "/**:\n  ros__parameters:\n    width: 640\n    height: 480\n    format: RGB888\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	if _, err := setCameraResolutionYAML("format: RGB888\n", 640, 480); err == nil {
		t.Fatal("expected an error without a ros__parameters section")
	}
}
