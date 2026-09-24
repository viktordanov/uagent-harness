package embedded

import (
	"encoding/json/v2"
	"fmt"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/tool"

	"github.com/viktordanov/uagent-harness/internal/images"
)

// pastedImages rewrites a request with the images pasted into its user
// messages, read from the image store in the state directory.
func (e *Engine) pastedImages(req llm.Request) llm.Request {
	return withImages(req, images.Store{Dir: images.DirIn(e.cfg.StateDir)}.DataURL)
}

// withImages gives the model the images pasted into user messages. The
// runner's llm.Message holds text only (unreal-agent v0.1.1), so a message
// carries its images as tag lines (internal/images), and each request is
// rewritten here: the message loses its tags, and each image follows it as a
// ViewImage call and its result, the one input the runner's Responses
// encoder sends images in. The rewrite depends only on the request, so the
// prompt cache still matches from one request to the next.
func withImages(req llm.Request, dataURL func(ref string) (string, error)) llm.Request {
	var out []llm.Item
	for i, item := range req.Input {
		msg, ok := item.Data.(llm.Message)
		if !ok || item.Type != llm.ItemMessage || msg.Role != llm.RoleUser {
			if out != nil {
				out = append(out, item)
			}

			continue
		}
		text, imgs := images.Split(msg.Text)
		if len(imgs) == 0 {
			if out != nil {
				out = append(out, item)
			}

			continue
		}
		if out == nil {
			out = append(make([]llm.Item, 0, len(req.Input)+2*len(imgs)), req.Input[:i]...)
		}
		msg.Text = text
		item.Data = msg
		out = append(out, item)
		for n, img := range imgs {
			out = append(out, imageItems(fmt.Sprintf("uah_image_%d_%d", i, n+1), img, dataURL)...)
		}
	}
	if out != nil {
		req.Input = out
	}

	return req
}

// imageItems are one pasted image as a ViewImage call and its result.
func imageItems(callID string, img images.Image, dataURL func(string) (string, error)) []llm.Item {
	args, _ := json.Marshal(map[string]string{"path": img.Label}) //nolint:errchkjson // a string map always encodes
	result := llm.ToolResult{CallID: callID}
	if url, err := dataURL(img.Ref); err != nil {
		result.Output = []llm.ToolResultOutput{{Kind: llm.ToolResultText, Value: "Error: " + img.Label + " pasted by the user is no longer available: " + err.Error()}}
	} else {
		result.Output = []llm.ToolResultOutput{
			{Kind: llm.ToolResultImage, Value: url},
			{Kind: llm.ToolResultText, Value: fmt.Sprintf("%s, pasted by the user with their message; %dx%d", img.Label, img.Width, img.Height)},
		}
	}

	return []llm.Item{
		{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: callID, Name: tool.ViewImageName, Arguments: string(args)}},
		{Type: llm.ItemToolResult, Data: result},
	}
}
