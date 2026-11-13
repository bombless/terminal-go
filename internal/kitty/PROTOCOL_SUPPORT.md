# Kitty Graphics Protocol support (MVP)

This implementation intentionally supports a bounded inline subset, not the full Kitty Graphics Protocol.

## Supported

- APC framing: ESC _ G ... ESC \\, including sequences split across VT parser Feed calls.
- Direct inline medium (t=d, also the protocol default).
- Actions a=T (transmit and place), a=t (transmit), a=p (place a transmitted image), a=d (delete by ID or delete all), and basic a=q status query.
- PNG payloads (f=100) and raw RGB/RGBA (f=24 / f=32).
- Base64 payloads and m=1 / m=0 chunk continuation.
- Image ID reuse, bounded decoded-image cache, bounded placement count, and basic success/error replies.
- Inline placements are expressed in terminal cells. If c / r are omitted, dimensions are estimated using the current fixed 10x20 pixel cell.
- Kitty cursor policy: by default (C=0 or omitted) the text cursor advances right by the placement columns and down by its rows, scrolling when it passes the bottom row. C=1 keeps the cursor in place.

## Not supported

- File transport (t=f), shared-memory transport, compression (including zlib), animation, and feature query (e=...).
- Full image deletion selectors, advanced z-index / clipping / composition semantics, scrollback image history, or separate alternate-screen image state.
- Exact cell pixel metrics from the native font; placement sizing uses the fixed 10x20 estimate.

Unsupported actions and formats must return an error response rather than being treated as successful.

## Placement and screen lifecycle semantics

- Image placements are maintained separately from character cells; APC payload bytes never become terminal text.
- A placement is anchored to the cursor cell that is current when the completed APC is handled. Subsequent cursor movement does not move an existing placement.
- Inline placements participate in the text flow: unless C=1, the cursor advances past the placement and the grid scrolls as needed, so following text starts below the image.
- Text scrolling moves placements with the grid. Images remain anchored to their content and are drawn at their scroll-adjusted position when viewing scrollback.
- Text erase / clear operations do not currently delete image placements. Use the protocol delete action to remove images.
- Window resize preserves placement cell coordinates and cell dimensions; the renderer clips anything outside the client area. The remote PTY is resized independently through the existing session path.
- Alternate-screen transitions do not maintain separate image sets; placements remain in the shared image manager and are drawn over whichever screen is active.

## Verification boundary

Unit and race tests cover APC parsing, parameter handling, complete and chunked inline transfer, PNG / RGB / RGBA decode, placement, deletion, cache accounting, and a deterministic parser-to-manager protocol fixture. A captured remote tview stream (784x1168 PNG payload, 214 APCs, 877,907 bytes) was replayed through the production parser and manager: one image and one placement were produced successfully.

The Windows GUI process starts successfully. The actual GUI SSH session, visual image output, deletion / resize interaction, and ordinary text / selection / cursor regression still require manual end-to-end acceptance. A no-PTY SSH capture is only a sender-output test; it cannot receive terminal protocol replies.
