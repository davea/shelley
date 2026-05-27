-- Drop the llm_requests table and its indexes. This debug feature has been removed.
DROP INDEX IF EXISTS idx_llm_requests_prefix_request_id;
DROP INDEX IF EXISTS idx_llm_requests_model;
DROP INDEX IF EXISTS idx_llm_requests_created_at;
DROP INDEX IF EXISTS idx_llm_requests_conversation_id;
DROP TABLE IF EXISTS llm_requests;
