/*
 * SWUpdate C Wrapper for Go CGO
 * 
 * This header defines the interface between C and Go.
 * It provides a simplified API that hides SWUpdate's internal structures
 * and manages C callbacks properly.
 */

#pragma once

#include <stdint.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

/*
 * Update source types
 */
typedef enum {
	UPDATE_SOURCE_UNKNOWN = 0,
	UPDATE_SOURCE_WEBSERVER,
	UPDATE_SOURCE_SURICATTA,
	UPDATE_SOURCE_DOWNLOADER,
	UPDATE_SOURCE_LOCAL,
	UPDATE_SOURCE_CHUNKS_DOWNLOADER
} UpdateSourceType;

/*
 * Update status
 */
typedef enum {
	UPDATE_STATUS_IDLE = 0,
	UPDATE_STATUS_START,
	UPDATE_STATUS_RUN,
	UPDATE_STATUS_SUCCESS,
	UPDATE_STATUS_FAILURE,
	UPDATE_STATUS_DOWNLOAD,
	UPDATE_STATUS_DONE,
	UPDATE_STATUS_SUBPROCESS,
	UPDATE_STATUS_PROGRESS
} UpdateStatus;

/*
 * Progress information structure
 * This is a stable structure that can be safely passed to Go
 */
typedef struct {
	UpdateStatus status;
	uint32_t dwl_percent;
	uint64_t dwl_bytes;
	uint32_t total_steps;
	uint32_t current_step;
	uint32_t step_percent;
	char current_image[256];
	char handler_name[64];
	char message[2048];
	UpdateSourceType source;
} SwupdateProgress;

/*
 * Result of an update operation
 */
typedef struct {
	int result_code;
	char message[1024];
} SwupdateResult;

/*
 * Callback types for Go integration
 * These are called from C into Go
 */
extern void goProgressCallback(SwupdateProgress *progress);
extern void goStatusCallback(UpdateStatus status, const char *message);
extern void goCompleteCallback(int result, const char *message);
extern int goReadCallback(char **buf, int *size);

/*
 * Initialize the SWUpdate wrapper
 * Must be called before any other function
 * Returns 0 on success, non-zero on error
 */
int swupdate_wrapper_init(const char *ctrl_socket_path, const char *prog_socket_path);

/*
 * Cleanup and release resources
 */
void swupdate_wrapper_cleanup(void);

/*
 * Start an async update
 * source_type: Type of update source
 * dry_run: If true, only simulate the update
 * software_set: Software set name (can be NULL)
 * running_mode: Running mode (can be NULL)
 * Returns: 0 on success, non-zero on error
 */
int swupdate_wrapper_start_update(
	UpdateSourceType source_type,
	bool dry_run,
	const char *software_set,
	const char *running_mode
);

/*
 * Send data chunk to SWUpdate
 * buf: Pointer to data buffer
 * size: Size of data in bytes
 * Returns: Number of bytes written, or negative on error
 */
int swupdate_wrapper_send_data(const char *buf, int size);

/*
 * Get current status
 * Returns the current update status
 */
UpdateStatus swupdate_wrapper_get_status(void);

/*
 * Cancel current update (if supported by SWUpdate)
 * Returns: 0 on success, non-zero on error or if not supported
 */
int swupdate_wrapper_cancel(void);

/*
 * Check if an update is currently running
 */
bool swupdate_wrapper_is_running(void);

/*
 * Wait for update completion (blocking)
 * timeout_ms: Timeout in milliseconds, 0 to wait indefinitely
 * Returns: Result code (0 for success, negative for error)
 */
int swupdate_wrapper_wait_completion(unsigned int timeout_ms);

/*
 * Set the read callback for streaming data from Go
 * This must be set before calling swupdate_wrapper_start_update
 */
void swupdate_wrapper_set_read_callback(void *context);

#ifdef __cplusplus
}
#endif
