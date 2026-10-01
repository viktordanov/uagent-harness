// The freeform tool's description and grammar are verbatim from
// openai/codex rust-v0.159.1 (Apache License 2.0, Copyright 2025 OpenAI):
// codex-rs/core/src/tools/handlers/apply_patch_spec.rs and
// codex-rs/core/assets/tools/apply_patch.lark.

package patch

import _ "embed"

// FreeformDescription is the freeform tool's description, as Codex offers
// it (create_apply_patch_freeform_tool).
const FreeformDescription = "The `apply_patch` tool can be used to edit files. This is a FREEFORM tool, so do not wrap the patch in JSON."

// Grammar is the Lark grammar the provider samples a freeform call's patch
// from.
//
//go:embed apply_patch.lark
var Grammar string
