package audio

// NodeKind classifies the functional role of an audio node.
type NodeKind uint8

const (
	KindSource NodeKind = iota // app playing audio
	KindMic                    // microphone / capture device
	KindSink                   // output device / speakers
)
