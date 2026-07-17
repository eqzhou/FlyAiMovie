package mediainfo

import "testing"

func TestParseFFProbeOutput(t *testing.T) {
	info, err := Parse([]byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"avg_frame_rate":"30000/1001"}],"format":{"duration":"3.500","format_name":"mov,mp4"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if info.Duration != 3.5 || info.Width != 1920 || info.Height != 1080 || info.Codec != "h264" || info.FrameRate < 29.9 {
		t.Fatalf("info=%+v", info)
	}
}
