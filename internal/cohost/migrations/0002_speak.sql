-- Add the per-channel "speak" switch. When on, a co-host reply is also spoken
-- aloud through the AI-voice overlay (using the channel's configured TTS voice)
-- in addition to being posted to chat. Defaults to 0 so existing channels keep
-- text-only behaviour until opt-in.
ALTER TABLE cohost_config ADD COLUMN speak INTEGER NOT NULL DEFAULT 0;
