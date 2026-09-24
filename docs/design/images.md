# Pasting images: research and plan

Status: researched, planned, and built 2026-09-24. Ledger item 37. Codex facts are from openai/codex at rust-v0.156.1 (`C/` is `codex-rs/`). Claude Code facts are from its public documentation. Runner facts are from `github.com/unreallabsai/unreal-agent` v0.1.1, and uagent facts from `github.com/viktordanov/uagent` v0.4.3.

1. [How Codex does it](#how-codex-does-it)
2. [How Claude Code does it](#how-claude-code-does-it)
3. [What the runner and uagent carry](#what-the-runner-and-uagent-carry)
4. [Options](#options)
5. [Design](#design)
6. [Open decisions](#open-decisions)

## How Codex does it

**The key.** Codex pastes an image when a `v` press has ctrl or alt (`C/tui/src/chatwidget/interaction.rs:145-171`). So ctrl+v, alt+v, and ctrl+alt+v all work, and macOS and Linux behave the same. The keymap reserves ctrl+v and ctrl+alt+v, and the user cannot rebind them (`C/tui/src/keymap.rs:2463-2464`). The shortcut overlay shows ctrl+alt+v on WSL and ctrl+v elsewhere (`C/tui/src/bottom_pane/shortcut_overlay.rs:35-42`). cmd+v on macOS is the terminal's text paste.

**The clipboard.** Codex reads the clipboard with the `arboard` crate only, with no `pbpaste`, `osascript`, `xclip`, or `wl-paste` fallback (`C/tui/src/clipboard_paste.rs:50-109`).

1. It first asks for a file list (a file copied in Finder), and opens the first file that decodes as an image.
2. Otherwise it reads the raw image.
3. It re-encodes the image as PNG into a temporary file (`:90-138`).
4. On WSL it falls back to PowerShell's `Get-Clipboard -Format Image` (`:139-229`).

A failure adds "Failed to paste image: …" to the history.

**A pasted or dropped path.** A drop is a pasted path to the terminal, so Codex has no separate drop code. `apply_paste` checks a paste of more than one character with `normalize_pasted_path` (`C/tui/src/bottom_pane/chat_composer/paste_input.rs:99-156`, `C/tui/src/clipboard_paste.rs:251-289`). That function:

- removes one pair of quotes;
- converts a `file://` URL;
- converts Windows paths under WSL;
- otherwise splits the text with `shlex` and accepts exactly one word, so `my\ file.png` works.

The path attaches when `image::image_dimensions` can read the file, and a space follows the placeholder. Choosing an image file after `@` also attaches it (`C/tui/src/bottom_pane/chat_composer.rs:2390-2500`).

**The composer.** Each image is an atomic element `[Image #N]` at the cursor (`C/protocol/src/models.rs:1680-1682`). Deleting the element drops the image, and the images left are renumbered so the numbers stay contiguous (`C/tui/src/bottom_pane/chat_composer/attachment_state.rs:196-224`). On submit, only the images whose placeholders are still in the text go out. The text keeps its placeholders (`C/tui/src/chatwidget/input_submission.rs:243-283`).

**Limits.** Codex scales an image to fit 2048×2048. PNG, JPEG, and WebP pass through when they fit; anything else becomes PNG, and JPEG quality is 85 (`C/utils/image/src/lib.rs:26, 162-217`).

**The wire.** A local image becomes three content items of the user message: `input_text "<image name=[Image #1] path=\"…\">"`, `input_image` with a `data:` URL and `detail: "high"`, and `input_text "</image>"` (`C/protocol/src/models.rs:1843-1865, 2003-2045`). A failed read becomes text instead of an image.

**The transcript.** The user's history cell keeps the text with its placeholders and the image paths (`C/tui/src/history_cell/messages.rs:15-240`). A resumed session rebuilds the cell from the saved input (`C/tui/src/chatwidget/user_messages.rs:766-810`).

## How Claude Code does it

- **The key.** ctrl+v pastes an image. It is cmd+v in iTerm2 and alt+v on Windows and WSL; WSL binds both ([interactive mode](https://code.claude.com/docs/en/interactive-mode), [keybindings](https://code.claude.com/docs/en/keybindings): `chat:imagePaste`, which the user can rebind).
- **The placeholder.** The paste inserts an `[Image #N]` chip at the cursor. An attachments context removes it with backspace or delete.
- **Paths.** You can drag and drop an image, paste it, or give its path in the prompt ("Analyze this image: /path/to/your/image.png"), which the model then reads with its tools ([common workflows](https://code.claude.com/docs/en/common-workflows)).

## What the runner and uagent carry

A user message cannot carry an image through the runner's types. Tool results can.

| Layer | Type | Image? |
| --- | --- | --- |
| uagent | `core.UserInput{ID, Text}` (`core/run.go:12`) | Text only |
| runner inbox | `inbox.Input{ID, Kind, Payload jsontext.Value}` (`harness/inbox/inbox.go`) | Any JSON |
| runner context builder | `AddExternalInput` decodes the payload as a JSON string into `llm.Message{Role: user, Text}` (`harness/contextbuilder/builder.go:43-61`) | No: another payload fails to decode |
| runner `llm` | `llm.Message{Role, Text, Phase}` (`harness/llm/model.go`) | No image parts |
| runner `llm` | `llm.ToolResult{CallID, Output []ToolResultOutput}` with `ToolResultImage` (`harness/llm/model.go`) | Yes: a data URL |
| Responses encoder | A user message becomes `input_text` only; a tool result with `ToolResultImage` becomes `function_call_output` with `input_image` (`harness/llm/responsesapi/request.go`) | In tool results only |
| session store | Stores the inbox input and the built items | Follows the types above |

So the runner blocks an image in the user message itself, at three places: the context builder's payload decoding, `llm.Message`, and the encoder. The runner's own ViewImage tool shows that the model layer and the providers take images in tool results: `viewimage` returns `data:<mime>;base64,…` as a `ToolResultImage` (`harness/tool/viewimage/viewimage.go:107`), with limits of 2000×2000 and 5,000,000 base64 bytes minus 1,000 (`:14-18`).

uagent does not block anything the runner allows. `core.UserInput` is text only, but the embedded engine turns it into the inbox's JSON string, and that is all the builder accepts. Changing uagent alone would not help, so uagent stays unchanged.

A probe on openai-codex (gpt-6-sol, 2026-09-24) confirmed the provider accepts the shape uah uses. The probe sent a user message, then a `ViewImage` function call and its output with a 64×64 blue PNG as `input_image`. The model answered "Blue", with 105 input tokens.

## Options

1. **Change the runner.** Give `llm.Message` content parts, let `AddExternalInput` decode a structured payload, and let the encoder send `input_image` in a user message. This matches Codex's wire exactly. The runner stays unchanged by rule, so this is written down, not made. The change it needs:
   - `harness/llm`: `Message` gets `Images []string` (data URLs), or `Parts []ContentPart` with text and image kinds.
   - `harness/contextbuilder`: `AddExternalInput` accepts `{"text": …, "images": [data URL, …]}` besides a JSON string.
   - `harness/llm/responsesapi`: `requestInputItem` and `requestItem` encode a user message with images as an `InputMessage` with `input_text` and `input_image` parts.
   - uagent: `core.UserInput` gets `Images []string`, and the process engine's runner input writes the structured payload.
2. **Rewrite the request in uah.** The embedded engine already owns the `llm.Adapter` in front of the provider client (`switcher`). It can put a pasted image into each request as the runner's own image input: a `ViewImage` call and its result after the user's message. This needs no runner or uagent change.
3. **Rewrite the HTTP body.** Build every provider client with a transport that edits the JSON body. uah would have to reproduce each provider's client (headers, credentials), which are the runner's.
4. **Refuse.** Tell the user to give a path, and let the model open it with ViewImage, as Claude Code's third way does.

## Design

uah takes option 2 and keeps option 1 as the clean upstream change (see [Open decisions](#open-decisions)).

### What goes through the session

A message keeps the user's text with its placeholders, and gets one tag line per image at its end:

```text
compare [Image #1] with [Image #2]

<uah-image label="[Image #1]" ref="<sha256>.png" size="1280x800"/>
<uah-image label="[Image #2]" ref="<sha256>.jpg" size="640x480"/>
```

The text takes the same path as any message: session, engine, runner inbox, and session store. So queueing, steering, hooks, the run records, and resume need no change. `internal/images` owns the format:

- `Join` adds the tags.
- `Split` takes them off.
- `Display` is the text without the tags, for the transcript, the queue, the picker, and the index.

### The image store

`internal/images.Store` keeps each image as `<state>/images/<sha256>.<png|jpg>`, named by its content, so a resumed session finds it again. `Prepare` works like the runner's ViewImage, so a pasted image and an image the model opens are sized alike:

1. It decodes PNG, JPEG, GIF, WebP, BMP, and TIFF, and refuses more than 32,000,000 pixels or a file over 64 MiB.
2. It scales the image down to fit 2000×2000.
3. A JPEG that needs no scaling passes through. Anything else becomes PNG, and a PNG over 5,000,000 base64 bytes becomes JPEG at quality 85.
4. An image still over the limit is refused with a notice.

### The model request

The embedded engine's switcher rewrites every request, compaction summaries included (`embedded/images.go`). A user message with tags loses them. Each image then follows the message as a `ViewImage` call with `{"path": "[Image #1]"}` and its result: the image's data URL and the line "[Image #1], pasted by the user with their message; W×H". The call IDs (`uah_image_<item>_<n>`) come from the item's place in the request, so the rewrite is the same on every request and the prompt cache still matches. A missing file becomes an error text in the result, and the pair stays valid.

### The TUI

| Input | Result |
| --- | --- |
| ctrl+v or alt+v | Reads the clipboard's image and inserts `[Image #N] ` at the cursor. A clipboard without an image pastes its text, as ctrl+v did before |
| A pasted or dropped path of one image file (quoted, shell-escaped, `file://`, or `~/`) | Attaches the file, as Codex does. Other pastes stay text |
| `@` and an image file from the menu | Attaches the file in place of its path, as Codex does |
| backspace at the end of a placeholder | Deletes the whole placeholder, and its image with it |

- **Numbering.** A new image takes the next number after those in the draft. Numbers are not renumbered after a delete (see [Open decisions](#open-decisions)).
- **Send.** On send, only the images whose placeholders are still in the text go out. ↑ takes a queued message back with its images.
- **Clipboard readers.** `internal/images/clipboard` reads the clipboard without cgo:
  - macOS: `osascript`. It tries a file copied in Finder (`«class furl»`, image files only), then `«class PNGf»`, then `«class TIFF»`, and decodes osascript's `«data …»` hex.
  - Linux: `wl-paste` in a Wayland session, else `xclip` under X11. Each lists the types, reads the first image type, or else the first image file of a `text/uri-list`.
  - When neither tool is installed, the notice says to install wl-clipboard or xclip, or to paste the path.
  - Other systems say pasting an image is not supported there.

All commands run through an `Exec` function, so tests use a fake.

### Engines

`engine.Capabilities.Images` is true on the embedded engine. The capability table has a row for "pasted images". On the process engine, the table's notice ("pasted images: not supported by the process engine (an image cannot be attached to a message; the model can still open an image file with its ViewImage tool); use the embedded engine") shows when the user pastes an image or an image path, and a pasted path stays text. `/status` and `uah doctor` list the gap as for any other row. A session with images resumed on the process engine sends the tags as text.

## Open decisions

1. **The upstream change.** Option 1 would send the image inside the user message, as Codex does, instead of as a tool result that the model did not call. The request shape then matches Codex and a subagent without ViewImage still has a valid history. Default taken: the uah rewrite, because the runner stays unchanged and the probe shows the provider accepts it. The change the runner needs is listed under [Options](#options).
2. **The key on Windows and WSL.** Claude Code uses alt+v there, and Codex shows ctrl+alt+v on WSL. Default taken: ctrl+v and alt+v on every system. Windows and WSL have no clipboard reader yet, so the notice asks for a path.
3. **Renumbering.** Codex renumbers the images left after a delete, and rewrites their placeholders. Default taken: no renumbering. The composer is a plain textarea, and rewriting the text would move the cursor. Numbers stay unique within a draft.
4. **A typed path.** Claude Code's third way (a path in the prompt) already works in uah: the model opens it with ViewImage. Default taken: a typed path stays text. Only a pasted or dropped path, or an `@` choice, attaches.
5. **Models without image input.** Codex warns when the model cannot take images. uah's model catalog has no input modalities. Default taken: send, and let the provider's error show.
6. **Store cleanup.** Images stay in `<state>/images` while any session may name them. Default taken: no cleanup. A later `uah sessions prune` could remove files that no session names.
