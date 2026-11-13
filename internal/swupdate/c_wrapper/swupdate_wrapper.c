/*
 * SWUpdate C Wrapper Implementation
 * 
 * This file implements the C wrapper for SWUpdate IPC interface.
 * It uses the official SWUpdate client library functions and
 * provides a simplified interface for Go via CGO.
 * 
 * Key design decisions:
 * - Uses static buffers for data transfer to avoid memory management issues
 * - All callbacks are handled internally in C
 * - Progress is read from the Unix domain socket separately
 */

#include "swupdate_wrapper.h"
#include "network_ipc.h"
#include "progress_ipc.h"
#include <string.h>
#include <stdlib.h>
#include <pthread.h>
#include <stdbool.h>
#include <unistd.h>
#include <fcntl.h>
#include <errno.h>

/*
 * Global state for the wrapper
 */
typedef struct {
	bool initialized;
	bool running;
	int connfd;
	int update_result;
	char ctrl_socket_path[256];
	char prog_socket_path[256];
	FILE *data_file;          // For streaming from file
	char *data_buffer;        // For streaming from memory
	size_t data_buffer_size;
	size_t data_buffer_offset;
	pthread_mutex_t mutex;
} WrapperState;

static WrapperState state = {
	.initialized = false,
	.running = false,
	.connfd = -1,
	.update_result = 0,
	.ctrl_socket_path = "/tmp/sockinstctrl",
	.prog_socket_path = "/tmp/swupdateprog",
	.data_file = NULL,
	.data_buffer = NULL,
	.data_buffer_size = 0,
	.data_buffer_offset = 0,
	.mutex = PTHREAD_MUTEX_INITIALIZER,
};

/*
 * Progress callback state
 */
typedef struct {
	bool connected;
	int socket_fd;
	SwupdateProgress last_progress;
} ProgressState;

static ProgressState progress_state = {
	.connected = false,
	.socket_fd = -1,
	.last_progress = {0},
};

/*
 * Callbacks for SWUpdate library
 */

/*
 * writedata callback - called by SWUpdate to get data chunks
 */
static int writedata_callback(char **buf, int *size) {
	int ret = 0;
	
	pthread_mutex_lock(&state.mutex);
	
	if (state.data_file) {
		/* Read from file */
		static char file_buf[4096];
		ret = fread(file_buf, 1, sizeof(file_buf), state.data_file);
		if (ret > 0) {
			*buf = file_buf;
			*size = ret;
		} else {
			*buf = NULL;
			*size = 0;
		}
	} else if (state.data_buffer && state.data_buffer_offset < state.data_buffer_size) {
		/* Read from memory buffer */
		size_t remaining = state.data_buffer_size - state.data_buffer_offset;
		ret = (int)(remaining < sizeof(int) ? remaining : 4096);
		*buf = state.data_buffer + state.data_buffer_offset;
		*size = ret;
		state.data_buffer_offset += ret;
	} else {
		/* No more data */
		*buf = NULL;
		*size = 0;
		ret = 0;
	}
	
	pthread_mutex_unlock(&state.mutex);
	return ret;
}

/*
 * getstatus callback - called by SWUpdate to report status
 */
static int getstatus_callback(ipc_message *msg) {
	/* Just acknowledge the status */
	state.update_result = msg->data.status.current;
	return 0;
}

/*
 * terminated callback - called when update completes
 */
static int terminated_callback(RECOVERY_STATUS status) {
	pthread_mutex_lock(&state.mutex);
	state.running = false;
	state.update_result = status;
	pthread_mutex_unlock(&state.mutex);
	
	/* Close data file if open */
	if (state.data_file) {
		fclose(state.data_file);
		state.data_file = NULL;
	}
	
	return 0;
}

/*
 * Public wrapper functions
 */

int swupdate_wrapper_init(const char *ctrl_socket_path, const char *prog_socket_path) {
	pthread_mutex_lock(&state.mutex);
	
	if (state.initialized) {
		pthread_mutex_unlock(&state.mutex);
		return 0; /* Already initialized */
	}
	
	/* Copy socket paths */
	if (ctrl_socket_path) {
		strncpy(state.ctrl_socket_path, ctrl_socket_path, sizeof(state.ctrl_socket_path) - 1);
		state.ctrl_socket_path[sizeof(state.ctrl_socket_path) - 1] = '\0';
	}
	if (prog_socket_path) {
		strncpy(state.prog_socket_path, prog_socket_path, sizeof(state.prog_socket_path) - 1);
		state.prog_socket_path[sizeof(state.prog_socket_path) - 1] = '\0';
	}
	
	/* Set environment variables for socket paths */
	SOCKET_CTRL_PATH = (char *)state.ctrl_socket_path;
	SOCKET_PROGRESS_PATH = (char *)state.prog_socket_path;
	
	state.initialized = true;
	state.running = false;
	state.connfd = -1;
	state.update_result = 0;
	state.data_file = NULL;
	state.data_buffer = NULL;
	state.data_buffer_size = 0;
	state.data_buffer_offset = 0;
	
	pthread_mutex_unlock(&state.mutex);
	return 0;
}

void swupdate_wrapper_cleanup(void) {
	pthread_mutex_lock(&state.mutex);
	
	if (state.connfd >= 0) {
		ipc_end(state.connfd);
		state.connfd = -1;
	}
	
	if (state.data_file) {
		fclose(state.data_file);
		state.data_file = NULL;
	}
	
	if (state.data_buffer) {
		free(state.data_buffer);
		state.data_buffer = NULL;
	}
	
	state.initialized = false;
	state.running = false;
	state.data_buffer_size = 0;
	state.data_buffer_offset = 0;
	
	pthread_mutex_unlock(&state.mutex);
	
	/* Cleanup progress state */
	if (progress_state.socket_fd >= 0) {
		close(progress_state.socket_fd);
		progress_state.socket_fd = -1;
	}
	progress_state.connected = false;
}

int swupdate_wrapper_start_update(
	UpdateSourceType source_type,
	bool dry_run,
	const char *software_set,
	const char *running_mode
) {
	pthread_mutex_lock(&state.mutex);
	
	if (!state.initialized) {
		pthread_mutex_unlock(&state.mutex);
		return -1; /* Not initialized */
	}
	
	if (state.running) {
		pthread_mutex_unlock(&state.mutex);
		return -2; /* Already running */
	}
	
	/* Prepare request */
	struct swupdate_request req;
	swupdate_prepare_req(&req);
	
	/* Set source type */
	req.source = (sourcetype)source_type;
	
	/* Set dry run flag */
	if (dry_run) {
		req.dry_run = RUN_DRYRUN;
	}
	
	/* Set software set and running mode */
	if (software_set) {
		strncpy(req.software_set, software_set, sizeof(req.software_set) - 1);
		req.software_set[sizeof(req.software_set) - 1] = '\0';
	}
	if (running_mode) {
		strncpy(req.running_mode, running_mode, sizeof(req.running_mode) - 1);
		req.running_mode[sizeof(req.running_mode) - 1] = '\0';
	}
	
	/* Start async update */
	state.running = true;
	state.update_result = 0;
	
	int result = swupdate_async_start(
		writedata_callback,
		getstatus_callback,
		terminated_callback,
		&req,
		sizeof(req)
	);
	
	if (result < 0) {
		state.running = false;
		pthread_mutex_unlock(&state.mutex);
		return result;
	}
	
	state.connfd = result;
	pthread_mutex_unlock(&state.mutex);
	return 0;
}

int swupdate_wrapper_send_data(const char *buf, int size) {
	pthread_mutex_lock(&state.mutex);
	
	if (!state.initialized || !state.running || state.connfd < 0) {
		pthread_mutex_unlock(&state.mutex);
		return -1;
	}
	
	/* Copy data to buffer */
	if (size > 0) {
		char *new_buffer = realloc(state.data_buffer, state.data_buffer_size + size);
		if (!new_buffer) {
			pthread_mutex_unlock(&state.mutex);
			return -1;
		}
		state.data_buffer = new_buffer;
		memcpy(state.data_buffer + state.data_buffer_size, buf, size);
		state.data_buffer_size += size;
	}
	
	pthread_mutex_unlock(&state.mutex);
	return size;
}

UpdateStatus swupdate_wrapper_get_status(void) {
	pthread_mutex_lock(&state.mutex);
	UpdateStatus status;
	
	/* Map SWUpdate RECOVERY_STATUS to our UpdateStatus */
	switch (state.update_result) {
	case IDLE:
		status = UPDATE_STATUS_IDLE;
		break;
	case START:
		status = UPDATE_STATUS_START;
		break;
	case RUN:
		status = UPDATE_STATUS_RUN;
		break;
	case SUCCESS:
		status = UPDATE_STATUS_SUCCESS;
		break;
	case FAILURE:
		status = UPDATE_STATUS_FAILURE;
		break;
	case DOWNLOAD:
		status = UPDATE_STATUS_DOWNLOAD;
		break;
	case DONE:
		status = UPDATE_STATUS_DONE;
		break;
	case SUBPROCESS:
		status = UPDATE_STATUS_SUBPROCESS;
		break;
	case PROGRESS:
		status = UPDATE_STATUS_PROGRESS;
		break;
	default:
		status = UPDATE_STATUS_IDLE;
		break;
	}
	
	pthread_mutex_unlock(&state.mutex);
	return status;
}

bool swupdate_wrapper_is_running(void) {
	pthread_mutex_lock(&state.mutex);
	bool running = state.running;
	pthread_mutex_unlock(&state.mutex);
	return running;
}

int swupdate_wrapper_cancel(void) {
	/* SWUpdate IPC doesn't support cancel in the current version */
	return -1; /* Not supported */
}

int swupdate_wrapper_wait_completion(unsigned int timeout_ms) {
	/* Not implemented - use progress socket instead */
	return -1;
}

void swupdate_wrapper_set_read_callback(void *context) {
	/* Not used in this implementation */
	(void)context;
}

/*
 * Open a file for streaming to SWUpdate
 */
int swupdate_wrapper_open_file(const char *filename) {
	pthread_mutex_lock(&state.mutex);
	
	if (state.data_file) {
		fclose(state.data_file);
		state.data_file = NULL;
	}
	
	state.data_file = fopen(filename, "rb");
	if (!state.data_file) {
		pthread_mutex_unlock(&state.mutex);
		return -1;
	}
	
	pthread_mutex_unlock(&state.mutex);
	return 0;
}

/*
 * Close the current data file
 */
void swupdate_wrapper_close_file(void) {
	pthread_mutex_lock(&state.mutex);
	
	if (state.data_file) {
		fclose(state.data_file);
		state.data_file = NULL;
	}
	
	pthread_mutex_unlock(&state.mutex);
}

/*
 * Set data buffer for in-memory streaming
 */
int swupdate_wrapper_set_buffer(const char *buf, size_t size) {
	pthread_mutex_lock(&state.mutex);
	
	if (state.data_buffer) {
		free(state.data_buffer);
		state.data_buffer = NULL;
	}
	
	if (buf && size > 0) {
		state.data_buffer = malloc(size);
		if (!state.data_buffer) {
			pthread_mutex_unlock(&state.mutex);
			return -1;
		}
		memcpy(state.data_buffer, buf, size);
		state.data_buffer_size = size;
		state.data_buffer_offset = 0;
	}
	
	pthread_mutex_unlock(&state.mutex);
	return 0;
}

/*
 * Progress socket functions
 */

int swupdate_progress_connect(void) {
	if (progress_state.connected) {
		return 0; /* Already connected */
	}
	
	progress_state.socket_fd = progress_ipc_connect(false);
	if (progress_state.socket_fd < 0) {
		return -1;
	}
	
	progress_state.connected = true;
	return 0;
}

void swupdate_progress_disconnect(void) {
	if (progress_state.socket_fd >= 0) {
		close(progress_state.socket_fd);
		progress_state.socket_fd = -1;
	}
	progress_state.connected = false;
}

int swupdate_progress_receive(SwupdateProgress *progress) {
	if (!progress_state.connected) {
		return -1;
	}
	
	struct progress_msg msg;
	int result = progress_ipc_receive(&progress_state.socket_fd, &msg);
	if (result < 0) {
		/* Try to reconnect */
		progress_state.connected = false;
		progress_state.socket_fd = progress_ipc_connect(false);
		if (progress_state.socket_fd >= 0) {
			progress_state.connected = true;
			result = progress_ipc_receive(&progress_state.socket_fd, &msg);
			if (result < 0) {
				return -1;
			}
		} else {
			return -1;
		}
	}
	
	/* Convert progress_msg to SwupdateProgress */
	progress->status = (UpdateStatus)msg.status;
	progress->dwl_percent = msg.dwl_percent;
	progress->dwl_bytes = msg.dwl_bytes;
	progress->total_steps = msg.nsteps;
	progress->current_step = msg.cur_step;
	progress->step_percent = msg.cur_percent;
	progress->source = (UpdateSourceType)msg.source;
	
	/* Copy strings safely */
	if (msg.cur_image[0] != '\0') {
		strncpy(progress->current_image, msg.cur_image, sizeof(progress->current_image) - 1);
		progress->current_image[sizeof(progress->current_image) - 1] = '\0';
	} else {
		progress->current_image[0] = '\0';
	}
	
	if (msg.hnd_name[0] != '\0') {
		strncpy(progress->handler_name, msg.hnd_name, sizeof(progress->handler_name) - 1);
		progress->handler_name[sizeof(progress->handler_name) - 1] = '\0';
	} else {
		progress->handler_name[0] = '\0';
	}
	
	if (msg.infolen > 0) {
		size_t copy_len = (msg.infolen < sizeof(progress->message) - 1) ? msg.infolen : sizeof(progress->message) - 1;
		memcpy(progress->message, msg.info, copy_len);
		progress->message[copy_len] = '\0';
	} else {
		progress->message[0] = '\0';
	}
	
	return 0;
}
