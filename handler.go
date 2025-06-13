package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/yutopp/go-flv"
	flvtag "github.com/yutopp/go-flv/tag"
	"github.com/yutopp/go-rtmp"
	rtmpmsg "github.com/yutopp/go-rtmp/message"
)

// Handler An RTMP connection handler.
//
// Connections
type Handler struct {
	rtmp.DefaultHandler
	flvFile *os.File
	flvEnc  *flv.Encoder
}

// Required to meet interface (Unused)
func (h *Handler) OnServe(conn *rtmp.Conn) {}

// Called when RTMP connection is established
func (h *Handler) OnConnect(timestamp uint32, cmd *rtmpmsg.NetConnectionConnect) error {
	log.Printf("New Connection")
	return nil
}

// Required to meet interface (Unused)
func (h *Handler) OnCreateStream(timestamp uint32, cmd *rtmpmsg.NetConnectionCreateStream) error {
	return nil
}

// Client is requesting to send a stream, complete inital setup
func (h *Handler) OnPublish(_ *rtmp.StreamContext, timestamp uint32, cmd *rtmpmsg.NetStreamPublish) error {
	log.Printf("Recieving Stream: %#v", cmd.PublishingName)

	// (example) Reject a connection when PublishingName is empty
	// if cmd.PublishingName == "" {
	// 	return errors.New("PublishingName is empty")
	// }

	// Record streams as FLV!
	os.MkdirAll("received", 0777)

	p := filepath.Join(
		"received/",
		filepath.Clean(filepath.Join("/", fmt.Sprintf("%s.flv", cmd.PublishingName))),
	)
	log.Printf("Saving to: %s", p)

	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return errors.Wrap(err, "Failed to create flv file")
	}
	h.flvFile = f

	enc, err := flv.NewEncoder(f, flv.FlagsAudio|flv.FlagsVideo)
	if err != nil {
		_ = f.Close()
		return errors.Wrap(err, "Failed to create flv encoder")
	}
	h.flvEnc = enc

	return nil
}

// Metadata from stream
func (h *Handler) OnSetDataFrame(timestamp uint32, data *rtmpmsg.NetStreamSetDataFrame) error {
	r := bytes.NewReader(data.Payload)

	var script flvtag.ScriptData
	if err := flvtag.DecodeScriptData(r, &script); err != nil {
		log.Printf("Failed to decode script data: Err = %+v", err)
		return nil // ignore
	}

	if err := h.flvEnc.Encode(&flvtag.FlvTag{
		TagType:   flvtag.TagTypeScriptData,
		Timestamp: timestamp,
		Data:      &script,
	}); err != nil {
		log.Printf("Failed to write script data: Err = %+v", err)
	}

	return nil
}

// Audio from stream
func (h *Handler) OnAudio(timestamp uint32, payload io.Reader) error {
	var audio flvtag.AudioData
	if err := flvtag.DecodeAudioData(payload, &audio); err != nil {
		return err
	}

	flvBody := new(bytes.Buffer)
	if _, err := io.Copy(flvBody, audio.Data); err != nil {
		return err
	}
	audio.Data = flvBody

	if err := h.flvEnc.Encode(&flvtag.FlvTag{
		TagType:   flvtag.TagTypeAudio,
		Timestamp: timestamp,
		Data:      &audio,
	}); err != nil {
		log.Printf("Failed to write audio: Err = %+v", err)
	}

	return nil
}

// Video from stream. Frames are processed here
func (h *Handler) OnVideo(timestamp uint32, payload io.Reader) error {
	var video flvtag.VideoData
	if err := flvtag.DecodeVideoData(payload, &video); err != nil {
		return err
	}

	flvBody := new(bytes.Buffer)
	if _, err := io.Copy(flvBody, video.Data); err != nil {
		return err
	}

	// Only process certain frame types (typically keyframes)
	// Check if this is a keyframe or a frame we want to process
	if video.FrameType == flvtag.FrameTypeKeyFrame {
		// Process the frame with computer vision
		processedData, err := h.processFrameWithCV(flvBody.Bytes(), video.CodecID)
		if err != nil {
			log.Printf("Failed to process video frame: Err = %+v", err)
			// Continue with original data if processing fails
		} else {
			// Replace with processed data
			flvBody = bytes.NewBuffer(processedData)
		}
	}

	video.Data = flvBody

	if err := h.flvEnc.Encode(&flvtag.FlvTag{
		TagType:   flvtag.TagTypeVideo,
		Timestamp: timestamp,
		Data:      &video,
	}); err != nil {
		log.Printf("Failed to write video: Err = %+v", err)
	}

	return nil
}

// Cleanup when connection closes
func (h *Handler) OnClose() {
	log.Printf("Connection Closed")

	if h.flvFile != nil {
		_ = h.flvFile.Close()
	}
}

/*
 *
 * Computer Vision Functions
 *
 */

// Process keyframe with Computer Vision
func (h *Handler) processFrameWithCV(frameData []byte, codecID flvtag.CodecID) ([]byte, error) {
	// For AVC/H.264
	if codecID == flvtag.CodecIDAVC {
		// Decode the AVC packet
		var avc flvtag.AVCVideoPacket
		if err := flvtag.DecodeAVCVideoPacket(bytes.NewReader(frameData), &avc); err != nil {
			return nil, err
		}

		// Only process video data (not sequence headers)
		if avc.AVCPacketType == flvtag.AVCPacketTypeNALU {
			// Extract frame from NAL units
			frame, err := h.extractFrameFromNALU(avc.Data)
			if err != nil {
				return nil, err
			}

			// Process the frame with GoCV
			processedFrame, err := h.applyComputerVision(frame)
			if err != nil {
				return nil, err
			}

			// Repackage the processed frame into NALUs
			processedNALU, err := h.packFrameToNALU(processedFrame)
			if err != nil {
				return nil, err
			}

			// Update the AVC packet with processed data
			avc.Data = bytes.NewReader(processedNALU)

			// Reserialize the AVC packet
			avcBuffer := new(bytes.Buffer)
			if err := flvtag.EncodeAVCVideoPacket(avcBuffer, &avc); err != nil {
				return nil, err
			}

			return avcBuffer.Bytes(), nil
		}
	}

	// Return original data for unhandled codecs or packet types
	return frameData, nil
}

// Extract image frame from NAL units
func (h *Handler) extractFrameFromNALU(naluData io.Reader) ([]byte, error) {
	// This would use a codec library like OpenH264 to decode the H.264 NAL units into raw frame data
	// Implementation depends on your specific codec library
	// Example placeholder:
	// return h.h264Decoder.DecodeNALU(naluData)

	// For now, this is a placeholder
	return io.ReadAll(naluData)
}

// Apply computer vision to the frame
func (h *Handler) applyComputerVision(frameData []byte) ([]byte, error) {
	// Convert frameData to an image format your CV library can work with
	// For example, if using GoCV (OpenCV bindings for Go):
	//
	// img, err := gocv.IMDecode(frameData, gocv.IMReadUnchanged)
	// if err != nil {
	//     return nil, err
	// }
	// defer img.Close()
	//
	// Apply your CV operations, e.g.:
	// gocv.CvtColor(img, &img, gocv.ColorBGRToGray)
	// gocv.Canny(img, &img, 100, 200)
	//
	// Convert back to bytes:
	// buf, err := gocv.IMEncode(".jpg", img)
	// return buf.GetBytes(), err

	// For now, this is a placeholder that returns the original data
	return frameData, nil
}

// Pack processed frame back into NAL units
func (h *Handler) packFrameToNALU(frameData []byte) ([]byte, error) {
	// This would use a codec library to encode the raw frame back into H.264 NAL units
	// Example placeholder:
	// return h.h264Encoder.EncodeFrame(frameData)

	// For now, this is a placeholder
	return frameData, nil
}
