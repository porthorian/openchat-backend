package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/openchat/openchat-backend/internal/rtc"
)

type options struct {
	backendURL    string
	serverID      string
	channelID     string
	filePath      string
	fileType      string
	mediaMode     string
	ffmpegBin     string
	userUID       string
	deviceID      string
	chunkBytes    int
	interval      time.Duration
	loop          bool
	exitAfterSend bool
	writeDir      string
}

type joinTicketResponse struct {
	Ticket       string `json:"ticket"`
	ServerID     string `json:"server_id"`
	ChannelID    string `json:"channel_id"`
	UserUID      string `json:"user_uid"`
	DeviceID     string `json:"device_id"`
	SignalingURL string `json:"signaling_url"`
}

type apiErrorResponse struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

type receivedStream struct {
	participantID string
	streamID      string
	fileType      string
	fileName      string
	totalSeq      int
	chunks        map[int][]byte
}

func main() {
	opts, err := parseFlags()
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid flags:", err)
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	join, err := requestJoinTicket(ctx, opts)
	if err != nil {
		logger.Error("join ticket request failed", "error", err)
		os.Exit(1)
	}

	logger.Info("join ticket issued",
		"server_id", join.ServerID,
		"channel_id", join.ChannelID,
		"user_uid", join.UserUID,
		"device_id", join.DeviceID,
		"signaling_url", join.SignalingURL,
	)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, join.SignalingURL, nil)
	if err != nil {
		logger.Error("signaling dial failed", "error", err)
		os.Exit(1)
	}

	var writeMu sync.Mutex
	send := func(envelope rtc.Envelope) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(envelope)
	}
	var shutdownOnce sync.Once
	shutdown := func(trigger string) {
		shutdownOnce.Do(func() {
			logger.Info("shutdown requested", "trigger", trigger)
			_ = send(rtc.NewEnvelope("rtc.leave", opts.channelID, "leave_"+uuid.NewString()[:8], map[string]any{
				"reason": trigger,
			}))
			_ = conn.Close()
		})
	}
	defer shutdown("defer")

	go func() {
		<-ctx.Done()
		shutdown("signal")
	}()

	if err := send(rtc.NewEnvelope("rtc.join", join.ChannelID, "join_"+uuid.NewString()[:8], map[string]any{
		"ticket": join.Ticket,
	})); err != nil {
		logger.Error("failed to send rtc.join", "error", err)
		os.Exit(1)
	}

	streamID := "stream_" + uuid.NewString()[:8]
	var streamBytes []byte
	if opts.filePath != "" && opts.mediaMode == "chunks" {
		streamBytes, err = os.ReadFile(opts.filePath)
		if err != nil {
			logger.Error("failed to read file", "path", opts.filePath, "error", err)
			os.Exit(1)
		}
		logger.Info("loaded transmit file", "path", opts.filePath, "bytes", len(streamBytes), "file_type", opts.fileType, "media_mode", opts.mediaMode)
	}
	if opts.filePath != "" && opts.mediaMode == "pcm-frames" {
		logger.Info("configured pcm frame transmission", "path", opts.filePath, "file_type", opts.fileType, "media_mode", opts.mediaMode)
	}

	received := make(map[string]*receivedStream)
	selfParticipantID := ""
	sendStarted := false
	startTransmit := func(trigger string) {
		if sendStarted || opts.filePath == "" {
			return
		}
		sendStarted = true
		logger.Info("starting media transmission", "trigger", trigger, "media_mode", opts.mediaMode, "loop", opts.loop)
		go func() {
			var transmitErr error
			if opts.mediaMode == "pcm-frames" {
				transmitErr = transmitPCMFrames(ctx, logger, send, opts, streamID)
			} else {
				transmitErr = transmitAudioState(ctx, logger, send, opts, streamID, streamBytes)
			}
			if transmitErr != nil {
				logger.Error("transmit failed", "error", transmitErr)
			}
			if opts.exitAfterSend {
				stop()
			}
		}()
	}

	for {
		var envelope rtc.Envelope
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		if err := conn.ReadJSON(&envelope); err != nil {
			if ctx.Err() != nil {
				logger.Info("shutdown complete")
				return
			}
			if isExpectedClose(err) {
				logger.Info("signaling closed")
				return
			}
			logger.Error("signaling read failed", "error", err)
			return
		}

		switch envelope.Type {
		case "rtc.joined":
			var payload struct {
				ParticipantID string `json:"participant_id"`
				Participants  []struct {
					ParticipantID string `json:"participant_id"`
					UserUID       string `json:"user_uid"`
				} `json:"participants"`
			}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				logger.Warn("failed to parse rtc.joined payload", "error", err)
				continue
			}
			selfParticipantID = strings.TrimSpace(payload.ParticipantID)
			logger.Info("joined channel", "participant_id", selfParticipantID, "existing_participants", len(payload.Participants))
			for _, peer := range payload.Participants {
				logger.Info("peer present", "participant_id", peer.ParticipantID, "user_uid", peer.UserUID)
			}
			if len(payload.Participants) > 0 {
				startTransmit("rtc.joined:existing_participant")
			} else if opts.filePath != "" {
				logger.Info("waiting for first listener before starting media transmission")
			}
		case "rtc.participant.joined":
			var payload struct {
				Participant struct {
					ParticipantID string `json:"participant_id"`
					UserUID       string `json:"user_uid"`
				} `json:"participant"`
			}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				logger.Warn("failed to parse participant.joined", "error", err)
				continue
			}
			logger.Info("participant joined", "participant_id", payload.Participant.ParticipantID, "user_uid", payload.Participant.UserUID)
			if payload.Participant.ParticipantID != "" && payload.Participant.ParticipantID != selfParticipantID {
				startTransmit("rtc.participant.joined")
			}
		case "rtc.participant.left":
			var payload struct {
				Participant struct {
					ParticipantID string `json:"participant_id"`
					UserUID       string `json:"user_uid"`
				} `json:"participant"`
			}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				logger.Warn("failed to parse participant.left", "error", err)
				continue
			}
			logger.Info("participant left", "participant_id", payload.Participant.ParticipantID, "user_uid", payload.Participant.UserUID)
		case "rtc.media.state":
			if len(envelope.Payload) == 0 {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				logger.Warn("failed to parse media.state payload", "error", err)
				continue
			}
			participantID := strings.TrimSpace(asString(payload["participant_id"]))
			if participantID == "" || participantID == selfParticipantID {
				continue
			}
			handleIncomingMediaState(logger, received, payload, opts.writeDir)
		case "rtc.error":
			logger.Warn("rtc error", "payload", string(envelope.Payload))
		case "rtc.pong":
			// keepalive response; no-op
		default:
			logger.Debug("ignoring event", "type", envelope.Type)
		}
	}
}

func parseFlags() (options, error) {
	var opts options
	var intervalMs int

	flag.StringVar(&opts.backendURL, "backend-url", "http://localhost:8080", "OpenChat backend base URL")
	flag.StringVar(&opts.serverID, "server-id", "srv_harbor", "server id to join")
	flag.StringVar(&opts.channelID, "channel-id", "", "voice channel id to join (required)")
	flag.StringVar(&opts.filePath, "file", "", "audio file path to transmit")
	flag.StringVar(&opts.fileType, "file-type", "", "file type label for transmitted data (required with --file)")
	flag.StringVar(&opts.mediaMode, "media-mode", "pcm-frames", "transmit mode: pcm-frames | chunks")
	flag.StringVar(&opts.ffmpegBin, "ffmpeg-bin", "ffmpeg", "ffmpeg binary path (used by --media-mode pcm-frames)")
	flag.StringVar(&opts.userUID, "user-uid", "", "user uid for join-ticket request")
	flag.StringVar(&opts.deviceID, "device-id", "", "device id for join-ticket request")
	flag.IntVar(&opts.chunkBytes, "chunk-bytes", 8192, "payload bytes per rtc.media.state chunk")
	flag.IntVar(&intervalMs, "interval-ms", 20, "interval between transmitted chunks in milliseconds")
	flag.BoolVar(&opts.loop, "loop", false, "loop file transmission forever")
	flag.BoolVar(&opts.exitAfterSend, "exit-after-send", false, "exit when one full file send completes")
	flag.StringVar(&opts.writeDir, "write-received-dir", "", "optional directory to write reconstructed incoming streams")
	flag.Parse()

	if strings.TrimSpace(opts.channelID) == "" {
		return opts, errors.New("--channel-id is required")
	}
	if opts.chunkBytes <= 0 || opts.chunkBytes > 256*1024 {
		return opts, errors.New("--chunk-bytes must be between 1 and 262144")
	}
	if intervalMs <= 0 || intervalMs > 5000 {
		return opts, errors.New("--interval-ms must be between 1 and 5000")
	}
	opts.interval = time.Duration(intervalMs) * time.Millisecond

	opts.backendURL = strings.TrimSpace(strings.TrimRight(opts.backendURL, "/"))
	if opts.backendURL == "" {
		return opts, errors.New("--backend-url is required")
	}
	if _, err := url.ParseRequestURI(opts.backendURL); err != nil {
		return opts, fmt.Errorf("invalid --backend-url: %w", err)
	}

	opts.channelID = strings.TrimSpace(opts.channelID)
	opts.serverID = strings.TrimSpace(opts.serverID)
	if opts.serverID == "" {
		opts.serverID = "srv_harbor"
	}

	opts.filePath = strings.TrimSpace(opts.filePath)
	opts.fileType = strings.TrimSpace(opts.fileType)
	opts.mediaMode = strings.TrimSpace(strings.ToLower(opts.mediaMode))
	if opts.filePath != "" && opts.fileType == "" {
		return opts, errors.New("--file-type is required when --file is provided")
	}
	switch opts.mediaMode {
	case "pcm-frames", "chunks":
	default:
		return opts, errors.New("--media-mode must be one of: pcm-frames, chunks")
	}
	if opts.mediaMode == "pcm-frames" && opts.filePath != "" {
		if _, err := exec.LookPath(opts.ffmpegBin); err != nil {
			return opts, fmt.Errorf("ffmpeg binary not found (%s): %w", opts.ffmpegBin, err)
		}
	}

	if opts.userUID == "" {
		opts.userUID = "uid_joiner_" + uuid.NewString()[:8]
	}
	if opts.deviceID == "" {
		opts.deviceID = "device_joiner_" + uuid.NewString()[:8]
	}
	return opts, nil
}

func requestJoinTicket(ctx context.Context, opts options) (joinTicketResponse, error) {
	var out joinTicketResponse

	endpoint := opts.backendURL + "/v1/rtc/channels/" + url.PathEscape(opts.channelID) + "/join-ticket"
	bodyBytes, _ := json.Marshal(map[string]any{"server_id": opts.serverID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenChat-User-UID", opts.userUID)
	req.Header.Set("X-OpenChat-Device-ID", opts.deviceID)

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		var apiErr apiErrorResponse
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Error.Message != "" {
			return out, fmt.Errorf("join ticket failed (%s): %s", apiErr.Error.Code, apiErr.Error.Message)
		}
		return out, fmt.Errorf("join ticket failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	if strings.TrimSpace(out.Ticket) == "" || strings.TrimSpace(out.SignalingURL) == "" {
		return out, errors.New("join ticket response missing ticket/signaling_url")
	}
	return out, nil
}

func transmitAudioState(
	ctx context.Context,
	logger *slog.Logger,
	send func(rtc.Envelope) error,
	opts options,
	streamID string,
	data []byte,
) error {
	totalSeq := (len(data) + opts.chunkBytes - 1) / opts.chunkBytes
	if totalSeq == 0 {
		logger.Warn("transmit skipped: empty file")
		return nil
	}
	fileName := filepath.Base(opts.filePath)

	loopIndex := 0
	for {
		loopIndex++
		logger.Info("starting transmit loop", "loop", loopIndex, "chunks", totalSeq)
		for seq := 0; seq < totalSeq; seq++ {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			start := seq * opts.chunkBytes
			end := start + opts.chunkBytes
			if end > len(data) {
				end = len(data)
			}
			chunkB64 := base64.StdEncoding.EncodeToString(data[start:end])
			payload := map[string]any{
				"stream_id":       streamID,
				"stream_kind":     "audio_file_chunks",
				"file_name":       fileName,
				"file_type":       opts.fileType,
				"loop_iteration":  loopIndex,
				"seq":             seq,
				"total_seq":       totalSeq,
				"chunk_b64":       chunkB64,
				"eof":             seq == totalSeq-1,
				"transmitted_at":  time.Now().UTC().Format(time.RFC3339Nano),
				"transmitter_uid": opts.userUID,
			}
			if err := send(rtc.NewEnvelope("rtc.media.state", opts.channelID, "media_"+strconv.Itoa(loopIndex)+"_"+strconv.Itoa(seq), payload)); err != nil {
				return err
			}
			time.Sleep(opts.interval)
		}
		logger.Info("completed transmit loop", "loop", loopIndex)

		if opts.exitAfterSend || !opts.loop {
			return nil
		}
	}
}

func transmitPCMFrames(
	ctx context.Context,
	logger *slog.Logger,
	send func(rtc.Envelope) error,
	opts options,
	streamID string,
) error {
	pcmBytes, err := decodeToPCM(ctx, opts.ffmpegBin, opts.filePath)
	if err != nil {
		return err
	}
	if len(pcmBytes) == 0 {
		logger.Warn("ffmpeg produced empty pcm output")
		return nil
	}

	frameSamples := int((48000 * opts.interval) / time.Second)
	if frameSamples <= 0 {
		frameSamples = 960
	}
	frameBytes := frameSamples * 2 // mono s16le
	totalSeq := (len(pcmBytes) + frameBytes - 1) / frameBytes
	fileName := filepath.Base(opts.filePath)

	loopIndex := 0
	for {
		loopIndex++
		logger.Info("starting pcm transmit loop", "loop", loopIndex, "frames", totalSeq, "frame_bytes", frameBytes)

		for seq := 0; seq < totalSeq; seq++ {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			start := seq * frameBytes
			end := start + frameBytes
			if end > len(pcmBytes) {
				end = len(pcmBytes)
			}
			chunkB64 := base64.StdEncoding.EncodeToString(pcmBytes[start:end])
			payload := map[string]any{
				"stream_id":         streamID,
				"stream_kind":       "audio_pcm_s16le_48k_mono",
				"file_name":         fileName,
				"file_type":         "pcm_s16le",
				"source_file_type":  opts.fileType,
				"loop_iteration":    loopIndex,
				"seq":               seq,
				"total_seq":         totalSeq,
				"chunk_b64":         chunkB64,
				"sample_rate_hz":    48000,
				"channels":          1,
				"frame_duration_ms": int(opts.interval / time.Millisecond),
				"eof":               seq == totalSeq-1,
				"transmitted_at":    time.Now().UTC().Format(time.RFC3339Nano),
				"transmitter_uid":   opts.userUID,
			}
			if err := send(rtc.NewEnvelope("rtc.media.state", opts.channelID, "pcm_"+strconv.Itoa(loopIndex)+"_"+strconv.Itoa(seq), payload)); err != nil {
				return err
			}
			time.Sleep(opts.interval)
		}

		logger.Info("completed pcm transmit loop", "loop", loopIndex)
		if opts.exitAfterSend || !opts.loop {
			return nil
		}
	}
}

func decodeToPCM(ctx context.Context, ffmpegBin string, inputPath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx,
		ffmpegBin,
		"-v", "error",
		"-i", inputPath,
		"-vn",
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ac", "1",
		"-ar", "48000",
		"pipe:1",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg stdout pipe failed: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start failed: %w", err)
	}

	output, readErr := io.ReadAll(stdout)
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, fmt.Errorf("ffmpeg output read failed: %w", readErr)
	}
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return nil, fmt.Errorf("ffmpeg decode failed: %s", msg)
	}
	return output, nil
}

func handleIncomingMediaState(
	logger *slog.Logger,
	streams map[string]*receivedStream,
	payload map[string]any,
	writeDir string,
) {
	participantID := strings.TrimSpace(asString(payload["participant_id"]))
	streamID := strings.TrimSpace(asString(payload["stream_id"]))
	fileType := strings.TrimSpace(asString(payload["file_type"]))
	fileName := strings.TrimSpace(asString(payload["file_name"]))
	if participantID == "" || streamID == "" {
		return
	}

	totalSeq, _ := asInt(payload["total_seq"])
	seq, _ := asInt(payload["seq"])
	chunkB64 := asString(payload["chunk_b64"])
	eof, _ := payload["eof"].(bool)

	streamKey := participantID + ":" + streamID
	stream := streams[streamKey]
	if stream == nil {
		stream = &receivedStream{
			participantID: participantID,
			streamID:      streamID,
			fileType:      fileType,
			fileName:      fileName,
			totalSeq:      totalSeq,
			chunks:        make(map[int][]byte),
		}
		streams[streamKey] = stream
	}

	if chunkB64 != "" {
		chunk, err := base64.StdEncoding.DecodeString(chunkB64)
		if err != nil {
			logger.Warn("failed to decode incoming chunk", "stream", streamKey, "seq", seq, "error", err)
			return
		}
		stream.chunks[seq] = chunk
	}
	logger.Info("received media chunk",
		"participant_id", participantID,
		"stream_id", streamID,
		"seq", seq,
		"total_seq", totalSeq,
		"eof", eof,
		"file_type", fileType,
	)

	if !eof {
		return
	}
	if len(stream.chunks) != stream.totalSeq {
		logger.Warn("stream ended with missing chunks",
			"stream", streamKey,
			"received", len(stream.chunks),
			"expected", stream.totalSeq,
		)
		return
	}
	if strings.TrimSpace(writeDir) == "" {
		logger.Info("stream complete (write disabled)", "stream", streamKey, "bytes", totalBytes(stream.chunks))
		return
	}

	if err := os.MkdirAll(writeDir, 0o755); err != nil {
		logger.Warn("failed to create write dir", "dir", writeDir, "error", err)
		return
	}
	filename := stream.fileName
	if filename == "" {
		filename = "incoming_" + participantID + "_" + streamID + ".bin"
	}
	outPath := filepath.Join(writeDir, filename)
	if ext := filepath.Ext(outPath); ext == "" && stream.fileType != "" {
		outPath += "." + sanitizeExtension(stream.fileType)
	}

	var assembled bytes.Buffer
	for seq := 0; seq < stream.totalSeq; seq++ {
		assembled.Write(stream.chunks[seq])
	}
	if err := os.WriteFile(outPath, assembled.Bytes(), 0o644); err != nil {
		logger.Warn("failed to write reconstructed stream", "path", outPath, "error", err)
		return
	}
	logger.Info("reconstructed stream written", "path", outPath, "bytes", assembled.Len())
}

func sanitizeExtension(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		}
	}
	if out.Len() == 0 {
		return "bin"
	}
	return out.String()
}

func totalBytes(chunks map[int][]byte) int {
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}
	return total
}

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func asInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func isExpectedClose(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}
	if closeErr, ok := err.(*websocket.CloseError); ok {
		return closeErr.Code == websocket.CloseNormalClosure || closeErr.Code == websocket.CloseGoingAway
	}
	return false
}
