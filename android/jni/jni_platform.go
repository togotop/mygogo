//go:build android && cgo

package jni

/*
#include <jni.h>
#include <android/log.h>
#include <stdlib.h>
#include <string.h>

// The JNI half of the Android platform features: the clipboard, opening a URL,
// notifications and the accessibility tree.
//
// # Two directions, and why this file needs the JavaVM
//
// jni.go's //export functions are Java calling Go, and each one already gets
// the JNIEnv it needs as its first argument. This file also has to call Java —
// the clipboard, an Intent and a NotificationManager are Java objects — and
// that is the direction JNI does not hand you anything for: a Go goroutine is
// not a thread the JVM knows, so it has no JNIEnv until it is attached to a
// JavaVM, and the JavaVM is only reachable from a thread that already has an
// env. The unlock is that Java calls nativeAttach once, at install, from a
// thread that has both: that call keeps a global reference to the JavaVM and to
// the MygoPlatform object, and every call below is made through them.
//
// # Threads
//
// mygo_platform_env attaches the calling Go thread on first use. On Android the
// caller is the backend's pump goroutine, which is locked to its OS thread for
// the life of the process, so it is attached once and never detached: detaching
// it would leave the backend unable to reach Java afterwards, and a thread that
// lives as long as the process leaks nothing by staying attached.
//
// Every call below is synchronous, which is what the platform.Clipboard and
// platform.Shell contracts want. The Java side is what hops to the Android UI
// thread where Android insists on it (announcements, accessibility events, the
// notification permission dialog) — doing that here would mean this file
// knowing about a Looper, and the Java side already has one.

// The JavaVM and the MygoPlatform object, set by nativeAttach.
static JavaVM *mygo_platform_vm = NULL;
static jobject mygo_platform_obj = NULL;

// The methods of MygoPlatform this file calls. They are looked up once, in
// nativeAttach, and every call checks its slot before using it: a Java method
// that was renamed is then a "the platform is not attached" error, not a crash
// inside JNI.
static jmethodID mygo_mid_read_text = NULL;
static jmethodID mygo_mid_read_html = NULL;
static jmethodID mygo_mid_mimes = NULL;
static jmethodID mygo_mid_write = NULL;
static jmethodID mygo_mid_clear = NULL;
static jmethodID mygo_mid_open = NULL;
static jmethodID mygo_mid_notify_allowed = NULL;
static jmethodID mygo_mid_network_status = NULL;
static jmethodID mygo_mid_multicast = NULL;
static jmethodID mygo_mid_wifi_info = NULL;
static jmethodID mygo_mid_start_foreground = NULL;
static jmethodID mygo_mid_stop_foreground = NULL;
static jmethodID mygo_mid_location = NULL;
static jmethodID mygo_mid_update_widget = NULL;
static jmethodID mygo_mid_battery_exempt = NULL;
static jmethodID mygo_mid_request_battery_exemption = NULL;
static jmethodID mygo_mid_secure_screen = NULL;
static jmethodID mygo_mid_system_proxy = NULL;
static jmethodID mygo_mid_update_tile = NULL;
static jmethodID mygo_mid_speak = NULL;
static jmethodID mygo_mid_stop_speaking = NULL;
static jmethodID mygo_mid_ble_start = NULL;
static jmethodID mygo_mid_ble_stop = NULL;
static jmethodID mygo_mid_ble_take = NULL;
static jmethodID mygo_mid_ble_connect = NULL;
static jmethodID mygo_mid_ble_disconnect = NULL;
static jmethodID mygo_mid_ble_read = NULL;
static jmethodID mygo_mid_ble_write = NULL;
static jmethodID mygo_mid_ble_subscribe = NULL;
static jmethodID mygo_mid_ble_bond = NULL;
static jmethodID mygo_mid_ble_mtu = NULL;
static jmethodID mygo_mid_nfc_available = NULL;
static jmethodID mygo_mid_nfc_start = NULL;
static jmethodID mygo_mid_nfc_stop = NULL;
static jmethodID mygo_mid_nfc_take = NULL;
static jmethodID mygo_mid_nfc_write = NULL;
static jmethodID mygo_mid_nfc_transceive = NULL;
static jmethodID mygo_mid_host_event = NULL;
static jmethodID mygo_mid_hce_start = NULL;
static jmethodID mygo_mid_hce_stop = NULL;
static jmethodID mygo_mid_hce_take = NULL;
static jmethodID mygo_mid_hce_respond = NULL;
static jmethodID mygo_mid_audio_volume = NULL;
static jmethodID mygo_mid_audio_set = NULL;
static jmethodID mygo_mid_audio_key = NULL;
static jmethodID mygo_mid_audio_ringer = NULL;
static jmethodID mygo_mid_silence_allowed = NULL;
static jmethodID mygo_mid_silence_settings = NULL;
static jmethodID mygo_mid_wifi_scan = NULL;
static jmethodID mygo_mid_wifi_take = NULL;
static jmethodID mygo_mid_wifi_join = NULL;
static jmethodID mygo_mid_wifi_leave = NULL;
static jmethodID mygo_mid_notify = NULL;
static jmethodID mygo_mid_cancel = NULL;
static jmethodID mygo_mid_cancel_all = NULL;
static jmethodID mygo_mid_announce = NULL;
static jmethodID mygo_mid_set_tree = NULL;
static jmethodID mygo_mid_secret_set = NULL;
static jmethodID mygo_mid_secret_get = NULL;
static jmethodID mygo_mid_secret_delete = NULL;
static jmethodID mygo_mid_scan = NULL;
static jmethodID mygo_mid_scan_cancel = NULL;
static jmethodID mygo_mid_take_scan = NULL;
static jmethodID mygo_mid_haptic = NULL;
static jmethodID mygo_mid_status_bar = NULL;
static jmethodID mygo_mid_pick_photos = NULL;
static jmethodID mygo_mid_export_file = NULL;
static jmethodID mygo_mid_take_media = NULL;
static jmethodID mygo_mid_register_push = NULL;
static jmethodID mygo_mid_permission_status = NULL;
static jmethodID mygo_mid_request_permission = NULL;
static jmethodID mygo_mid_open_settings = NULL;
static jmethodID mygo_mid_take_permission = NULL;
static jmethodID mygo_mid_keep_awake = NULL;
static jmethodID mygo_mid_on_battery = NULL;
static jmethodID mygo_mid_device_info = NULL;
static jmethodID mygo_mid_speech_start = NULL;
static jmethodID mygo_mid_speech_stop = NULL;
static jmethodID mygo_mid_speech_cancel = NULL;
static jmethodID mygo_mid_take_speech = NULL;

// mygo_platform_exception clears a pending Java exception and reports whether
// there was one.
//
// Every call below clears one. A pending exception that survives the JNI call
// that raised it poisons the thread: the next CallXxxMethod returns immediately
// with a default value and does nothing, which reads as "the clipboard is
// empty" everywhere. The Java methods are written to catch what they raise and
// answer with a value, so this is the belt to that suspenders.
static int mygo_platform_exception(JNIEnv *env) {
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		return 1;
	}
	return 0;
}

// mygo_platform_env returns a JNIEnv for the calling thread, attaching it to the
// JavaVM the first time. NULL means the Java side never attached.
static JNIEnv *mygo_platform_env(void) {
	JNIEnv *env = NULL;
	if (mygo_platform_vm == NULL) {
		return NULL;
	}
	jint rc = (*mygo_platform_vm)->GetEnv(mygo_platform_vm, (void **)&env, JNI_VERSION_1_6);
	if (rc == JNI_EDETACHED) {
		// jni.h's C mapping takes JNIEnv** here, unlike GetEnv's void** a
		// line above: the two really are declared differently.
		rc = (*mygo_platform_vm)->AttachCurrentThread(mygo_platform_vm, &env, NULL);
	}
	if (rc != JNI_OK) {
		return NULL;
	}
	return env;
}

// mygo_platform_ready reports whether nativeAttach has run and every method this
// file calls was found.
static jint mygo_platform_ready(void) {
	if (mygo_platform_vm == NULL || mygo_platform_obj == NULL || mygo_mid_read_text == NULL) {
		return 0;
	}
	return 1;
}

// mygo_platform_java_bytes copies the bytes of a byte[] argument of a //export
// function into memory malloc'd here, with the length as its result. The caller
// frees *out with free(). A NULL array is length 0 and *out NULL.
//
// The length is read here rather than passed as a parameter because the JNI
// signature of a native method taking byte[] has no length in it: the JVM calls
// Java_org_mygo_MygoPlatform_nativeNotificationClicked(JNIEnv*, jobject,
// jbyteArray), and a fourth Go parameter would be reading whatever the caller's
// registers happen to hold. (The generated libmygo.h is what caught this: see
// the //export functions below.)
static int mygo_platform_java_bytes(JNIEnv *env, jbyteArray a, char **out) {
	*out = NULL;
	if (env == NULL || a == NULL) {
		return 0;
	}
	jsize n = (*env)->GetArrayLength(env, a);
	if (n < 0) {
		mygo_platform_exception(env);
		return 0;
	}
	char *buf = (char *)malloc(n > 0 ? (size_t)n : 1);
	if (buf == NULL) {
		return 0;
	}
	if (n > 0) {
		(*env)->GetByteArrayRegion(env, a, 0, n, (jbyte *)buf);
		if (mygo_platform_exception(env)) {
			free(buf);
			return 0;
		}
	}
	*out = buf;
	return (int)n;
}

// mygo_platform_bytes makes a Java byte[] of len bytes at data, or NULL when
// data is NULL (which the callers use to mean "this representation is absent").
//
// # Why bytes and not a jstring
//
// NewStringUTF takes *modified* UTF-8: it cannot carry a NUL and it mangles the
// supplementary characters (emoji, and a good deal of CJK-extended text) that
// JNI hands back as two surrogate halves. Clipboard text is exactly the place
// where those characters show up, so the two sides exchange UTF-8 bytes and the
// Java side decodes them with Charsets.UTF_8 — the one encoding that survives
// the trip whole.
static jbyteArray mygo_platform_bytes(JNIEnv *env, const char *data, int len) {
	if (data == NULL) {
		return NULL;
	}
	jbyteArray a = (*env)->NewByteArray(env, (jsize)len);
	if (a == NULL) {
		return NULL;
	}
	if (len > 0) {
		(*env)->SetByteArrayRegion(env, a, 0, (jsize)len, (const jbyte *)data);
		if (mygo_platform_exception(env)) {
			(*env)->DeleteLocalRef(env, a);
			return NULL;
		}
	}
	return a;
}

// mygo_platform_arg is mygo_platform_bytes for an argument that is required but
// may be empty: a NULL data pointer becomes an empty array rather than "absent".
// An empty string is a value a user can copy, and the two sides must not
// confuse it with no value at all.
static jbyteArray mygo_platform_arg(JNIEnv *env, const char *data, int len) {
	return mygo_platform_bytes(env, data != NULL ? data : "", len);
}

// mygo_platform_grab copies the Java byte[] a into memory malloc'd here, with
// its length in *out_len. NULL means the method returned nothing — no value, or
// a call that failed. The caller frees the result with free().
//
// An empty array copies to a 1-byte buffer so that its non-NULL result keeps
// meaning "the value is here and it is empty", which is how the Go side tells an
// empty string from an absent one.
static char *mygo_platform_grab(JNIEnv *env, jbyteArray a, int *out_len) {
	*out_len = 0;
	if (a == NULL) {
		return NULL;
	}
	jsize n = (*env)->GetArrayLength(env, a);
	if (n < 0) {
		mygo_platform_exception(env);
		return NULL;
	}
	char *out = (char *)malloc(n > 0 ? (size_t)n : 1);
	if (out == NULL) {
		return NULL;
	}
	if (n > 0) {
		(*env)->GetByteArrayRegion(env, a, 0, n, (jbyte *)out);
		if (mygo_platform_exception(env)) {
			free(out);
			return NULL;
		}
	}
	*out_len = (int)n;
	return out;
}

// mygo_platform_call_bytes calls a no-argument method that returns byte[] and
// returns a malloc'd copy of the result.
static char *mygo_platform_call_bytes(jmethodID mid, int *out_len) {
	*out_len = 0;
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mid == NULL) {
		return NULL;
	}
	jbyteArray a = (jbyteArray)(*env)->CallObjectMethod(env, mygo_platform_obj, mid);
	if (mygo_platform_exception(env)) {
		return NULL;
	}
	char *out = mygo_platform_grab(env, a, out_len);
	if (a != NULL) {
		(*env)->DeleteLocalRef(env, a);
	}
	return out;
}

// mygo_platform_call_int calls a method taking one byte[] and returning int.
// -1 means "the call did not happen" (no Java side, or a Java exception).
static jint mygo_platform_call_int(jmethodID mid, const char *data, int len) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mid == NULL) {
		return -1;
	}
	jbyteArray a = mygo_platform_arg(env, data, len);
	if (a == NULL) {
		return -1;
	}
	jint rc = (*env)->CallIntMethod(env, mygo_platform_obj, mid, a);
	(*env)->DeleteLocalRef(env, a);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return rc;
}

// mygo_platform_call_bytes_arg calls a method taking one byte[] and returning
// byte[], which is how a raw NFC exchange answers. NULL means no answer.
static char *mygo_platform_call_bytes_arg(jmethodID mid, const char *data, int len, int *out_len) {
	*out_len = 0;
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mid == NULL) {
		return NULL;
	}
	jbyteArray arg = mygo_platform_arg(env, data, len);
	if (arg == NULL) {
		return NULL;
	}
	jbyteArray a = (jbyteArray)(*env)->CallObjectMethod(env, mygo_platform_obj, mid, arg);
	(*env)->DeleteLocalRef(env, arg);
	if (mygo_platform_exception(env)) {
		return NULL;
	}
	char *out = mygo_platform_grab(env, a, out_len);
	if (a != NULL) {
		(*env)->DeleteLocalRef(env, a);
	}
	return out;
}

// mygo_platform_call_bool calls a method taking one byte[] and returning
// boolean. -1 is "the call did not happen", 1 true and 0 false.
static jint mygo_platform_call_bool(jmethodID mid, const char *data, int len) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mid == NULL) {
		return -1;
	}
	jbyteArray a = mygo_platform_arg(env, data, len);
	if (a == NULL) {
		return -1;
	}
	jboolean ok = (*env)->CallBooleanMethod(env, mygo_platform_obj, mid, a);
	(*env)->DeleteLocalRef(env, a);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return ok == JNI_TRUE ? 1 : 0;
}

// mygo_platform_call_void calls a method taking one byte[] and returning void.
// -1 is "the call did not happen".
static jint mygo_platform_call_void(jmethodID mid, const char *data, int len) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mid == NULL) {
		return -1;
	}
	jbyteArray a = mygo_platform_arg(env, data, len);
	if (a == NULL) {
		return -1;
	}
	(*env)->CallVoidMethod(env, mygo_platform_obj, mid, a);
	(*env)->DeleteLocalRef(env, a);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return 0;
}

// mygo_platform_call_two_bool calls writeClipboard(byte[] plain, byte[] html)
// where either argument may be NULL (the representation is absent).
// The secure store: secretSet(namespace, key, value, accessibility),
// secretGet(namespace, key) -> byte[] or NULL, and delete. They are separate
// wrappers rather than one shape because the return differs: two booleans, or
// bytes. All three run the Android Keystore on the Java side, which may take
// milliseconds on the first call — an unwrap — so their Go callers treat them
// like any other native call and do not hold a lock across them.
static jint mygo_platform_call_secret_set(jmethodID mid, const char *ns, int ns_len, const char *key, int key_len, const char *value, int value_len, int accessibility) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mid == NULL) {
		return -1;
	}
	jbyteArray a = mygo_platform_arg(env, ns, ns_len);
	jbyteArray b = mygo_platform_arg(env, key, key_len);
	jbyteArray c = mygo_platform_arg(env, value, value_len);
	if (a == NULL || b == NULL || c == NULL) {
		if (a != NULL) (*env)->DeleteLocalRef(env, a);
		if (b != NULL) (*env)->DeleteLocalRef(env, b);
		if (c != NULL) (*env)->DeleteLocalRef(env, c);
		return -1;
	}
	jboolean ok = (*env)->CallBooleanMethod(env, mygo_platform_obj, mid, a, b, c, (jint)accessibility);
	(*env)->DeleteLocalRef(env, a);
	(*env)->DeleteLocalRef(env, b);
	(*env)->DeleteLocalRef(env, c);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return ok == JNI_TRUE ? 0 : 1;
}
static char *mygo_platform_call_secret_get(jmethodID mid, const char *ns, int ns_len, const char *key, int key_len, int *out_len) {
	*out_len = 0;
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mid == NULL) {
		return NULL;
	}
	jbyteArray a = mygo_platform_arg(env, ns, ns_len);
	jbyteArray b = mygo_platform_arg(env, key, key_len);
	if (a == NULL || b == NULL) {
		if (a != NULL) (*env)->DeleteLocalRef(env, a);
		if (b != NULL) (*env)->DeleteLocalRef(env, b);
		return NULL;
	}
	jbyteArray r = (jbyteArray)(*env)->CallObjectMethod(env, mygo_platform_obj, mid, a, b);
	(*env)->DeleteLocalRef(env, a);
	(*env)->DeleteLocalRef(env, b);
	if (mygo_platform_exception(env)) {
		return NULL;
	}
	char *out = mygo_platform_grab(env, r, out_len);
	if (r != NULL) {
		(*env)->DeleteLocalRef(env, r);
	}
	return out;
}
static jint mygo_platform_call_secret_delete(jmethodID mid, const char *ns, int ns_len, const char *key, int key_len) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mid == NULL) {
		return -1;
	}
	jbyteArray a = mygo_platform_arg(env, ns, ns_len);
	jbyteArray b = mygo_platform_arg(env, key, key_len);
	if (a == NULL || b == NULL) {
		if (a != NULL) (*env)->DeleteLocalRef(env, a);
		if (b != NULL) (*env)->DeleteLocalRef(env, b);
		return -1;
	}
	jboolean ok = (*env)->CallBooleanMethod(env, mygo_platform_obj, mid, a, b);
	(*env)->DeleteLocalRef(env, a);
	(*env)->DeleteLocalRef(env, b);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return ok == JNI_TRUE ? 0 : 1;
}
static jint mygo_platform_call_two_bool(jmethodID mid, int has_one, const char *one, int one_len,
	int has_two, const char *two, int two_len) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mid == NULL) {
		return -1;
	}
	jbyteArray a = has_one ? mygo_platform_arg(env, one, one_len) : NULL;
	jbyteArray b = has_two ? mygo_platform_arg(env, two, two_len) : NULL;
	if ((has_one && a == NULL) || (has_two && b == NULL)) {
		if (a != NULL) {
			(*env)->DeleteLocalRef(env, a);
		}
		if (b != NULL) {
			(*env)->DeleteLocalRef(env, b);
		}
		return -1;
	}
	jboolean ok = (*env)->CallBooleanMethod(env, mygo_platform_obj, mid, a, b);
	if (a != NULL) {
		(*env)->DeleteLocalRef(env, a);
	}
	if (b != NULL) {
		(*env)->DeleteLocalRef(env, b);
	}
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return ok == JNI_TRUE ? 1 : 0;
}

// Notifications cross as one JSON object: id, title, body, silent, urgent,
// progress and the action buttons. A positional signature grew a parameter per
// feature and could not carry a list at all.


// mygo_platform_attach records the JavaVM and the MygoPlatform object and looks
// up every method this file calls. It returns 0 on success, -1 when nothing
// could be recorded and -2 when a method is missing (a rename on the Java side).
static jint mygo_platform_attach(JNIEnv *env, jobject thiz) {
	if (env == NULL || thiz == NULL) {
		return -1;
	}
	if (mygo_platform_vm == NULL) {
		if ((*env)->GetJavaVM(env, &mygo_platform_vm) != JNI_OK) {
			return -1;
		}
	}
	if (mygo_platform_obj == NULL) {
		mygo_platform_obj = (*env)->NewGlobalRef(env, thiz);
		if (mygo_platform_obj == NULL) {
			return -1;
		}
	}
	jclass cls = (*env)->GetObjectClass(env, mygo_platform_obj);
	if (cls == NULL) {
		mygo_platform_exception(env);
		return -1;
	}
	struct {
		const char *name;
		const char *sig;
		jmethodID *slot;
	} lookups[] = {
		{"readClipboardText", "()[B", &mygo_mid_read_text},
		{"readClipboardHtml", "()[B", &mygo_mid_read_html},
		{"clipboardMimeTypes", "()[B", &mygo_mid_mimes},
		{"writeClipboard", "([B[B)Z", &mygo_mid_write},
		{"clearClipboard", "()Z", &mygo_mid_clear},
		{"openExternalUrl", "([B)I", &mygo_mid_open},
		{"notificationsAllowed", "()Z", &mygo_mid_notify_allowed},
		{"networkStatus", "()I", &mygo_mid_network_status},
		{"setMulticast", "(Z)I", &mygo_mid_multicast},
		{"wifiInfo", "()[B", &mygo_mid_wifi_info},
		{"startForeground", "([B)I", &mygo_mid_start_foreground},
		{"stopForeground", "()I", &mygo_mid_stop_foreground},
		{"lastLocation", "()[B", &mygo_mid_location},
		{"updateWidget", "([B)I", &mygo_mid_update_widget},
		{"batteryExempt", "()I", &mygo_mid_battery_exempt},
		{"requestBatteryExemption", "()I", &mygo_mid_request_battery_exemption},
		{"setSecureScreen", "(Z)I", &mygo_mid_secure_screen},
		{"systemProxy", "()[B", &mygo_mid_system_proxy},
		{"updateTile", "([B)I", &mygo_mid_update_tile},
		{"speak", "([B)I", &mygo_mid_speak},
		{"stopSpeaking", "()I", &mygo_mid_stop_speaking},
		{"bleStartScan", "([B)I", &mygo_mid_ble_start},
		{"bleStopScan", "()I", &mygo_mid_ble_stop},
		{"takeBleEvent", "()[B", &mygo_mid_ble_take},
		{"bleConnect", "([B)I", &mygo_mid_ble_connect},
		{"bleDisconnect", "([B)I", &mygo_mid_ble_disconnect},
		{"bleRead", "([B)I", &mygo_mid_ble_read},
		{"bleWrite", "([B)I", &mygo_mid_ble_write},
		{"bleSubscribe", "([B)I", &mygo_mid_ble_subscribe},
		{"bleCreateBond", "([B)I", &mygo_mid_ble_bond},
		{"bleRequestMtu", "([B)I", &mygo_mid_ble_mtu},
		{"nfcAvailable", "()Z", &mygo_mid_nfc_available},
		{"nfcScanStart", "()I", &mygo_mid_nfc_start},
		{"nfcScanStop", "()I", &mygo_mid_nfc_stop},
		{"takeNfcEvent", "()[B", &mygo_mid_nfc_take},
		{"nfcWriteText", "([B)I", &mygo_mid_nfc_write},
		{"nfcTransceive", "([B)[B", &mygo_mid_nfc_transceive},
		{"hostEvent", "([B)I", &mygo_mid_host_event},
		{"hceStart", "([B)I", &mygo_mid_hce_start},
		{"hceStop", "()I", &mygo_mid_hce_stop},
		{"takeHceRequest", "()[B", &mygo_mid_hce_take},
		{"hceRespond", "([B)I", &mygo_mid_hce_respond},
		{"audioVolume", "()[B", &mygo_mid_audio_volume},
		{"audioSetVolume", "([B)I", &mygo_mid_audio_set},
		{"audioMediaKey", "([B)I", &mygo_mid_audio_key},
		{"audioRinger", "([B)I", &mygo_mid_audio_ringer},
		{"silenceAllowed", "()Z", &mygo_mid_silence_allowed},
		{"openSilenceSettings", "()I", &mygo_mid_silence_settings},
		{"wifiScan", "()I", &mygo_mid_wifi_scan},
		{"takeWifiEvent", "()[B", &mygo_mid_wifi_take},
		{"wifiJoin", "([B)I", &mygo_mid_wifi_join},
		{"wifiLeave", "()I", &mygo_mid_wifi_leave},
		{"postNotification", "([B)I", &mygo_mid_notify},
		{"cancelNotification", "([B)Z", &mygo_mid_cancel},
		{"cancelAllNotifications", "()Z", &mygo_mid_cancel_all},
		{"announce", "([B)V", &mygo_mid_announce},
		{"setAccessibilityTree", "([B)V", &mygo_mid_set_tree},
		{"secretSet", "([B[B[BI)Z", &mygo_mid_secret_set},
		{"secretGet", "([B[B)[B", &mygo_mid_secret_get},
		{"secretDelete", "([B[B)Z", &mygo_mid_secret_delete},
		{"startScan", "(J[B)Z", &mygo_mid_scan},
		{"cancelScan", "(J)Z", &mygo_mid_scan_cancel},
		{"takeScanResult", "()Ljava/lang/String;", &mygo_mid_take_scan},
		{"haptic", "(I)I", &mygo_mid_haptic},
		{"setStatusBar", "(IZ)I", &mygo_mid_status_bar},
		{"pickPhotos", "(JZ)Z", &mygo_mid_pick_photos},
		{"exportFile", "(J[B[B)Z", &mygo_mid_export_file},
		{"takeMediaResult", "()Ljava/lang/String;", &mygo_mid_take_media},
		{"registerPush", "()I", &mygo_mid_register_push},
		{"permissionStatus", "(I)I", &mygo_mid_permission_status},
		{"requestPermission", "(IJ)I", &mygo_mid_request_permission},
		{"openAppSettings", "()I", &mygo_mid_open_settings},
		{"takePermissionResult", "()Ljava/lang/String;", &mygo_mid_take_permission},
		{"keepAwake", "(Z[BZ)I", &mygo_mid_keep_awake},
		{"onBattery", "()Z", &mygo_mid_on_battery},
		{"deviceInfo", "()[B", &mygo_mid_device_info},
		{"speechStart", "(J[BZ[B)I", &mygo_mid_speech_start},
		{"speechStop", "(J)Z", &mygo_mid_speech_stop},
		{"speechCancel", "(J)Z", &mygo_mid_speech_cancel},
		{"takeSpeechResult", "()Ljava/lang/String;", &mygo_mid_take_speech},
	};
	int count = (int)(sizeof(lookups) / sizeof(lookups[0]));
	for (int n = 0; n < count; n++) {
		*lookups[n].slot = (*env)->GetMethodID(env, cls, lookups[n].name, lookups[n].sig);
		if (*lookups[n].slot == NULL) {
			mygo_platform_exception(env);
			// The Java class is the other half of this contract; saying which
			// method is missing is what turns "attach failed" into a fix.
			__android_log_print(ANDROID_LOG_INFO, "mygo",
					    "platform bridge: %s %s is missing from the host",
					    lookups[n].name, lookups[n].sig);
			(*env)->DeleteLocalRef(env, cls);
			return -2;
		}
	}
	(*env)->DeleteLocalRef(env, cls);
	return 0;
}

// The wrappers the Go side calls. Each one returns -1 when the platform is not
// attached, so that the Go side can tell "Java said no" from "there is no Java
// side to ask".
static char *mygo_platform_read_text(int *out_len) { return mygo_platform_call_bytes(mygo_mid_read_text, out_len); }
static char *mygo_platform_read_html(int *out_len) { return mygo_platform_call_bytes(mygo_mid_read_html, out_len); }
static char *mygo_platform_mimes(int *out_len) { return mygo_platform_call_bytes(mygo_mid_mimes, out_len); }
static jint mygo_platform_write(int has_plain, const char *plain, int plain_len, int has_html, const char *html, int html_len) {
	return mygo_platform_call_two_bool(mygo_mid_write, has_plain, plain, plain_len, has_html, html, html_len);
}
static jint mygo_platform_clear(void) {
	return mygo_platform_call_bool(mygo_mid_clear, NULL, 0);
}
static jint mygo_platform_open(const char *url, int url_len) {
	return mygo_platform_call_int(mygo_mid_open, url, url_len);
}
static jint mygo_platform_notify_allowed(void) {
	return mygo_platform_call_bool(mygo_mid_notify_allowed, NULL, 0);
}
static jint mygo_platform_set_multicast(int on) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_multicast == NULL) {
		return -1;
	}
	jint rc = (*env)->CallIntMethod(env, mygo_platform_obj, mygo_mid_multicast, on ? JNI_TRUE : JNI_FALSE);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return rc;
}
static char *mygo_platform_wifi_info(int *out_len) { return mygo_platform_call_bytes(mygo_mid_wifi_info, out_len); }
static jint mygo_platform_start_foreground(const char *reason, int reason_len) {
	return mygo_platform_call_int(mygo_mid_start_foreground, reason, reason_len);
}
static jint mygo_platform_stop_foreground(void) {
	return mygo_platform_call_int(mygo_mid_stop_foreground, NULL, 0);
}
static char *mygo_platform_location(int *out_len) { return mygo_platform_call_bytes(mygo_mid_location, out_len); }
static char *mygo_platform_system_proxy(int *out_len) { return mygo_platform_call_bytes(mygo_mid_system_proxy, out_len); }
static jint mygo_platform_update_tile(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_update_tile, json, json_len);
}
static jint mygo_platform_speak(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_speak, json, json_len);
}
static jint mygo_platform_stop_speaking(void) {
	return mygo_platform_call_int(mygo_mid_stop_speaking, NULL, 0);
}
static jint mygo_platform_ble_start(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_ble_start, json, json_len);
}
static jint mygo_platform_ble_stop(void) {
	return mygo_platform_call_int(mygo_mid_ble_stop, NULL, 0);
}
static char *mygo_platform_ble_take(int *out_len) {
	return mygo_platform_call_bytes(mygo_mid_ble_take, out_len);
}
// The GATT methods all take one JSON object and answer with a status code.
static jint mygo_platform_ble_connect(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_ble_connect, json, json_len);
}
static jint mygo_platform_ble_disconnect(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_ble_disconnect, json, json_len);
}
static jint mygo_platform_ble_read(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_ble_read, json, json_len);
}
static jint mygo_platform_ble_write(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_ble_write, json, json_len);
}
static jint mygo_platform_ble_subscribe(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_ble_subscribe, json, json_len);
}
static jint mygo_platform_ble_bond(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_ble_bond, json, json_len);
}
static jint mygo_platform_ble_mtu(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_ble_mtu, json, json_len);
}
static jint mygo_platform_nfc_available(void) {
	return mygo_platform_call_bool(mygo_mid_nfc_available, NULL, 0);
}
static jint mygo_platform_nfc_start(void) {
	return mygo_platform_call_int(mygo_mid_nfc_start, NULL, 0);
}
static jint mygo_platform_nfc_stop(void) {
	return mygo_platform_call_int(mygo_mid_nfc_stop, NULL, 0);
}
static char *mygo_platform_nfc_take(int *out_len) {
	return mygo_platform_call_bytes(mygo_mid_nfc_take, out_len);
}
static jint mygo_platform_nfc_write(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_nfc_write, json, json_len);
}
static char *mygo_platform_nfc_transceive(const char *json, int json_len, int *out_len) {
	return mygo_platform_call_bytes_arg(mygo_mid_nfc_transceive, json, json_len, out_len);
}
static jint mygo_platform_host_event(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_host_event, json, json_len);
}
static jint mygo_platform_hce_start(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_hce_start, json, json_len);
}
static jint mygo_platform_hce_stop(void) {
	return mygo_platform_call_int(mygo_mid_hce_stop, NULL, 0);
}
static char *mygo_platform_hce_take(int *out_len) {
	return mygo_platform_call_bytes(mygo_mid_hce_take, out_len);
}
static jint mygo_platform_hce_respond(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_hce_respond, json, json_len);
}
static char *mygo_platform_audio_volume(int *out_len) {
	return mygo_platform_call_bytes(mygo_mid_audio_volume, out_len);
}
static jint mygo_platform_audio_set(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_audio_set, json, json_len);
}
static jint mygo_platform_audio_key(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_audio_key, json, json_len);
}
static jint mygo_platform_audio_ringer(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_audio_ringer, json, json_len);
}
static jint mygo_platform_silence_allowed(void) {
	return mygo_platform_call_bool(mygo_mid_silence_allowed, NULL, 0);
}
static jint mygo_platform_silence_settings(void) {
	return mygo_platform_call_int(mygo_mid_silence_settings, NULL, 0);
}
static jint mygo_platform_wifi_scan(void) {
	return mygo_platform_call_int(mygo_mid_wifi_scan, NULL, 0);
}
static char *mygo_platform_wifi_take(int *out_len) {
	return mygo_platform_call_bytes(mygo_mid_wifi_take, out_len);
}
static jint mygo_platform_wifi_join(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_wifi_join, json, json_len);
}
static jint mygo_platform_wifi_leave(void) {
	return mygo_platform_call_int(mygo_mid_wifi_leave, NULL, 0);
}
static jint mygo_platform_update_widget(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_update_widget, json, json_len);
}
static jint mygo_platform_battery_exempt(void) {
	return mygo_platform_call_int(mygo_mid_battery_exempt, NULL, 0);
}
static jint mygo_platform_request_battery_exemption(void) {
	return mygo_platform_call_int(mygo_mid_request_battery_exemption, NULL, 0);
}
static jint mygo_platform_set_secure_screen(int on) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_secure_screen == NULL) {
		return -1;
	}
	jint rc = (*env)->CallIntMethod(env, mygo_platform_obj, mygo_mid_secure_screen, on ? JNI_TRUE : JNI_FALSE);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return rc;
}
static jint mygo_platform_network_status(void) {
	return mygo_platform_call_int(mygo_mid_network_status, NULL, 0);
}
static jint mygo_platform_notify(const char *json, int json_len) {
	return mygo_platform_call_int(mygo_mid_notify, json, json_len);
}
static jint mygo_platform_cancel(const char *id, int id_len) {
	return mygo_platform_call_bool(mygo_mid_cancel, id, id_len);
}
static jint mygo_platform_cancel_all(void) {
	return mygo_platform_call_bool(mygo_mid_cancel_all, NULL, 0);
}
static jint mygo_platform_announce(const char *text, int text_len) {
	return mygo_platform_call_void(mygo_mid_announce, text, text_len);
}
static jint mygo_platform_set_tree(const char *json, int json_len) {
	return mygo_platform_call_void(mygo_mid_set_tree, json, json_len);
}
static jint mygo_platform_secret_set(const char *ns, int ns_len, const char *key, int key_len, const char *value, int value_len, int accessibility) {
	return mygo_platform_call_secret_set(mygo_mid_secret_set, ns, ns_len, key, key_len, value, value_len, accessibility);
}
static char *mygo_platform_secret_get(const char *ns, int ns_len, const char *key, int key_len, int *out_len) {
	return mygo_platform_call_secret_get(mygo_mid_secret_get, ns, ns_len, key, key_len, out_len);
}
static jint mygo_platform_secret_delete(const char *ns, int ns_len, const char *key, int key_len) {
	return mygo_platform_call_secret_delete(mygo_mid_secret_delete, ns, ns_len, key, key_len);
}
// startScan(long token, byte[] prompt) opens the picker; the answer comes back
// through Java_org_mygo_MygoPlatform_nativeScanResult, not as a return value.
static jint mygo_platform_scan(jlong token, const char *prompt, int prompt_len) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_scan == NULL) {
		return -1;
	}
	jbyteArray p = mygo_platform_arg(env, prompt, prompt_len);
	if (p == NULL) {
		return -1;
	}
	jboolean ok = (*env)->CallBooleanMethod(env, mygo_platform_obj, mygo_mid_scan, token, p);
	(*env)->DeleteLocalRef(env, p);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return ok == JNI_TRUE ? 0 : 1;
}
// mygo_platform_take_scan_result asks the Java side for the pending scan
// result and copies it out as a malloc'd C string. It returns NULL when there
// is nothing to report; *code is then untouched.
static const char *mygo_platform_take_scan_result(void) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_take_scan == NULL) {
		return NULL;
	}
	jstring js = (jstring)(*env)->CallObjectMethod(env, mygo_platform_obj, mygo_mid_take_scan);
	if (mygo_platform_exception(env) || js == NULL) {
		return NULL;
	}
	const char *utf = (*env)->GetStringUTFChars(env, js, NULL);
	if (utf == NULL) {
		(*env)->DeleteLocalRef(env, js);
		return NULL;
	}
	char *out = strdup(utf);
	(*env)->ReleaseStringUTFChars(env, js, utf);
	(*env)->DeleteLocalRef(env, js);
	// 编码格式："<code>\n<token>\n<text>"，三样一次带走。
	return out;
}

static jint mygo_platform_scan_cancel(jlong token) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_scan_cancel == NULL) {
		return -1;
	}
	jboolean ok = (*env)->CallBooleanMethod(env, mygo_platform_obj, mygo_mid_scan_cancel, token);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return ok == JNI_TRUE ? 0 : 1;
}

// mygo_platform_permission_status asks permissionStatus(int) for the grant the
// system recorded: 0 granted, 1 denied, 2 not determined, 3 unsupported. -1
// means the call did not happen.
static jint mygo_platform_permission_status(jint kind) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_permission_status == NULL) {
		return -1;
	}
	jint rc = (*env)->CallIntMethod(env, mygo_platform_obj, mygo_mid_permission_status, kind);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return rc;
}

// mygo_platform_request_permission calls requestPermission(int, long): 0 the
// prompt is up (or the grant already exists and the answer is queued), 3 the
// prompt could not be shown at all. -1 means the call did not happen.
static jint mygo_platform_request_permission(jint kind, jlong token) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_request_permission == NULL) {
		return -1;
	}
	jint rc = (*env)->CallIntMethod(env, mygo_platform_obj, mygo_mid_request_permission, kind, token);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return rc;
}

// mygo_platform_open_settings calls openAppSettings(): 0 opened, 1 failed.
// -1 means the call did not happen.
static jint mygo_platform_open_settings(void) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_open_settings == NULL) {
		return -1;
	}
	jint rc = (*env)->CallIntMethod(env, mygo_platform_obj, mygo_mid_open_settings);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return rc;
}

// mygo_platform_speech_start calls speechStart(long, byte[], boolean, byte[]):
// 0 listening, 1 no speech service, 2 failed. -1 means the call did not happen.
// The language and the hints are UTF-8; the hints are newline-joined.
static jint mygo_platform_speech_start(jlong token, const char *language, int language_len,
				      jboolean on_device, const char *hints, int hints_len) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_speech_start == NULL) {
		return -1;
	}
	jbyteArray lang = mygo_platform_bytes(env, language, language_len);
	jbyteArray bias = mygo_platform_bytes(env, hints, hints_len);
	jint rc = (*env)->CallIntMethod(env, mygo_platform_obj, mygo_mid_speech_start, token, lang, on_device, bias);
	if (lang != NULL) {
		(*env)->DeleteLocalRef(env, lang);
	}
	if (bias != NULL) {
		(*env)->DeleteLocalRef(env, bias);
	}
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return rc;
}

// mygo_platform_speech_stop and mygo_platform_speech_cancel call
// speechStop(long) and speechCancel(long): 0 done, 1 no such session. -1 means
// the call did not happen.
static jint mygo_platform_speech_stop(jlong token) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_speech_stop == NULL) {
		return -1;
	}
	jboolean ok = (*env)->CallBooleanMethod(env, mygo_platform_obj, mygo_mid_speech_stop, token);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return ok == JNI_TRUE ? 0 : 1;
}

static jint mygo_platform_speech_cancel(jlong token) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_speech_cancel == NULL) {
		return -1;
	}
	jboolean ok = (*env)->CallBooleanMethod(env, mygo_platform_obj, mygo_mid_speech_cancel, token);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return ok == JNI_TRUE ? 0 : 1;
}

// mygo_platform_take_speech_result asks for a pending recognizer answer:
// "<token>\n<code>\n<text>". NULL when there is nothing to report.
static const char *mygo_platform_take_speech_result(void) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_take_speech == NULL) {
		return NULL;
	}
	jstring js = (jstring)(*env)->CallObjectMethod(env, mygo_platform_obj, mygo_mid_take_speech);
	if (mygo_platform_exception(env) || js == NULL) {
		return NULL;
	}
	const char *utf = (*env)->GetStringUTFChars(env, js, NULL);
	if (utf == NULL) {
		(*env)->DeleteLocalRef(env, js);
		return NULL;
	}
	char *out = strdup(utf);
	(*env)->ReleaseStringUTFChars(env, js, utf);
	(*env)->DeleteLocalRef(env, js);
	return out;
}

// mygo_platform_device_info asks for the phone's identity as UTF-8 JSON, so a
// device name the owner typed survives the crossing.
static char *mygo_platform_device_info(int *out_len) { return mygo_platform_call_bytes(mygo_mid_device_info, out_len); }

// mygo_platform_keep_awake calls keepAwake(boolean, byte[], boolean): 0 applied,
// 1 no window. -1 means the call did not happen.
static jint mygo_platform_keep_awake(jboolean display, const char *reason, int reason_len, jboolean on) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_keep_awake == NULL) {
		return -1;
	}
	jbyteArray arr = NULL;
	if (reason_len > 0) {
		arr = (*env)->NewByteArray(env, reason_len);
		if (arr == NULL) {
			mygo_platform_exception(env);
			return -1;
		}
		(*env)->SetByteArrayRegion(env, arr, 0, reason_len, (const jbyte *)reason);
	}
	jint rc = (*env)->CallIntMethod(env, mygo_platform_obj, mygo_mid_keep_awake, display, arr, on);
	if (arr != NULL) {
		(*env)->DeleteLocalRef(env, arr);
	}
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return rc;
}

// mygo_platform_on_battery calls onBattery(). A call that did not happen reads
// as "not on battery", which is the answer that changes no behaviour.
static jboolean mygo_platform_on_battery(void) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_on_battery == NULL) {
		return JNI_FALSE;
	}
	jboolean rc = (*env)->CallBooleanMethod(env, mygo_platform_obj, mygo_mid_on_battery);
	if (mygo_platform_exception(env)) {
		return JNI_FALSE;
	}
	return rc;
}

// mygo_platform_take_permission_result asks the Java side for a pending
// permission answer: "<token>\n<status>". NULL when there is nothing to report.
static const char *mygo_platform_take_permission_result(void) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_take_permission == NULL) {
		return NULL;
	}
	jstring js = (jstring)(*env)->CallObjectMethod(env, mygo_platform_obj, mygo_mid_take_permission);
	if (mygo_platform_exception(env) || js == NULL) {
		return NULL;
	}
	const char *utf = (*env)->GetStringUTFChars(env, js, NULL);
	if (utf == NULL) {
		(*env)->DeleteLocalRef(env, js);
		return NULL;
	}
	char *out = strdup(utf);
	(*env)->ReleaseStringUTFChars(env, js, utf);
	(*env)->DeleteLocalRef(env, js);
	return out;
}

// mygo_platform_haptic calls haptic(int): 0 done, 1 no vibrator, 2 failed.
// -1 means the call did not happen (no Java side, or a Java exception).
static jint mygo_platform_haptic(jint kind) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_haptic == NULL) {
		return -1;
	}
	jint rc = (*env)->CallIntMethod(env, mygo_platform_obj, mygo_mid_haptic, kind);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return rc;
}

// mygo_platform_set_status_bar calls setStatusBar(int, boolean): 0 done,
// 1 unsupported, 2 failed. -1 means the call did not happen.
static jint mygo_platform_set_status_bar(jint style, jboolean hidden) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_status_bar == NULL) {
		return -1;
	}
	jint rc = (*env)->CallIntMethod(env, mygo_platform_obj, mygo_mid_status_bar, style, hidden);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return rc;
}

// mygo_platform_pick_photos calls pickPhotos(long, boolean): 0 started,
// 1 the picker could not start. -1 means the call did not happen.
static jint mygo_platform_pick_photos(jlong token, jboolean multiple) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_pick_photos == NULL) {
		return -1;
	}
	jboolean ok = (*env)->CallBooleanMethod(env, mygo_platform_obj, mygo_mid_pick_photos, token, multiple);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return ok == JNI_TRUE ? 0 : 1;
}

// mygo_platform_export_file calls exportFile(long, byte[] path, byte[] title).
static jint mygo_platform_export_file(jlong token, const char *path, int path_len, const char *title, int title_len) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_export_file == NULL) {
		return -1;
	}
	jbyteArray p = mygo_platform_arg(env, path, path_len);
	jbyteArray t = mygo_platform_arg(env, title, title_len);
	if (p == NULL || t == NULL) {
		if (p != NULL) (*env)->DeleteLocalRef(env, p);
		if (t != NULL) (*env)->DeleteLocalRef(env, t);
		return -1;
	}
	jboolean ok = (*env)->CallBooleanMethod(env, mygo_platform_obj, mygo_mid_export_file, token, p, t);
	(*env)->DeleteLocalRef(env, p);
	(*env)->DeleteLocalRef(env, t);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return ok == JNI_TRUE ? 0 : 1;
}

// mygo_platform_take_media_result asks the Java side for a pending photo or
// export answer and copies it out as a malloc'd C string.
static const char *mygo_platform_take_media_result(void) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_take_media == NULL) {
		return NULL;
	}
	jstring js = (jstring)(*env)->CallObjectMethod(env, mygo_platform_obj, mygo_mid_take_media);
	if (mygo_platform_exception(env) || js == NULL) {
		return NULL;
	}
	const char *utf = (*env)->GetStringUTFChars(env, js, NULL);
	if (utf == NULL) {
		(*env)->DeleteLocalRef(env, js);
		return NULL;
	}
	char *out = strdup(utf);
	(*env)->ReleaseStringUTFChars(env, js, utf);
	(*env)->DeleteLocalRef(env, js);
	return out;
}

// mygo_platform_register_push calls registerPush(): 0 started, 1 unsupported,
// 2 failed. -1 means the call did not happen.
static jint mygo_platform_register_push(void) {
	JNIEnv *env = mygo_platform_env();
	if (env == NULL || mygo_platform_obj == NULL || mygo_mid_register_push == NULL) {
		return -1;
	}
	jint rc = (*env)->CallIntMethod(env, mygo_platform_obj, mygo_mid_register_push);
	if (mygo_platform_exception(env)) {
		return -1;
	}
	return rc;
}
*/
import "C"

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/android/backend"
	"github.com/egoist/mygo/internal/platform"
)

// init installs the bridge before Java has attached.
//
// The bridge is installed here rather than in nativeAttach so that the platform
// features exist from the moment the library is loaded: a call that arrives
// before install() then reports backend.ErrPlatformBridge — "the Java side did
// not attach" — instead of the package answering from the unsupported backend's
// silence, which is a failure with no explanation.
func init() {
	backend.SetPlatformBridge(androidJNI{})
}

// androidJNI is the Android implementation of backend.PlatformBridge: every
// method is a JNI call to org.mygo.MygoPlatform.
//
// The Java method names and signatures are the table inside
// mygo_platform_attach, and they are the same strings MygoPlatform.kt declares.
// javap over the compiled class is what checks the two against each other (see
// T34.md).
type androidJNI struct{}

// attached reports whether nativeAttach has run and every method was found.
func attached() bool { return C.mygo_platform_ready() != 0 }

// hapticIndex maps a HapticFeedback value to the number MygoPlatform.haptic
// takes. The two lists must stay in step; an unknown name is unsupported.
func hapticIndex(kind string) (C.jint, bool) {
	switch kind {
	case "selection":
		return 0, true
	case "light":
		return 1, true
	case "medium":
		return 2, true
	case "heavy":
		return 3, true
	case "soft":
		return 4, true
	case "rigid":
		return 5, true
	case "success":
		return 6, true
	case "warning":
		return 7, true
	case "error":
		return 8, true
	}
	return 0, false
}

// statusBarStyle maps a StatusBarStyle value to the number MygoPlatform
// takes: "" auto, "light" light text, "dark" dark text.
func statusBarStyle(style string) (C.jint, bool) {
	switch style {
	case "":
		return 0, true
	case "light":
		return 1, true
	case "dark":
		return 2, true
	}
	return 0, false
}

// SetStatusBar applies the bar style and visibility; 1 from the Java side is
// "this platform cannot", which is unsupported rather than a failure.
func (androidJNI) SetStatusBar(style string, hidden bool) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	n, ok := statusBarStyle(style)
	if !ok {
		return platform.ErrUnsupported
	}
	var h C.jboolean
	if hidden {
		h = 1
	}
	switch rc := C.mygo_platform_set_status_bar(n, h); rc {
	case 0:
		return nil
	case 1:
		return platform.ErrUnsupported
	default:
		return backend.ErrPlatformBridge
	}
}

// Haptic plays the platform's own feedback for kind. 1 from the Java side is
// "this device has no vibrator", which is unsupported rather than a failure.
func (androidJNI) Haptic(kind string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	n, ok := hapticIndex(kind)
	if !ok {
		return platform.ErrUnsupported
	}
	switch rc := C.mygo_platform_haptic(n); rc {
	case 0:
		return nil
	case 1:
		return platform.ErrUnsupported
	default:
		return backend.ErrPlatformBridge
	}
}

// ClipboardRead reads the clip's text, its markup and the MIME types it
// advertises.
func (androidJNI) ClipboardRead() (backend.ClipboardPayload, []string, error) {
	if !attached() {
		return backend.ClipboardPayload{}, nil, backend.ErrPlatformBridge
	}
	var p backend.ClipboardPayload
	var n C.int
	if s, ok := takeJavaBytes(C.mygo_platform_read_text(&n), n); ok {
		p.Plain, p.HasPlain = s, true
	}
	if s, ok := takeJavaBytes(C.mygo_platform_read_html(&n), n); ok {
		p.HTML, p.HasHTML = s, true
	}
	mimes, ok := takeJavaBytes(C.mygo_platform_mimes(&n), n)
	if !ok {
		return p, nil, nil
	}
	// One MIME type per line: the Java side joins them because a MIME type
	// cannot contain a newline, and a byte[] of lines keeps this side of the
	// JNI to one shape (see mygo_platform_bytes).
	var out []string
	for _, line := range strings.Split(mimes, "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return p, out, nil
}

// ClipboardWrite replaces the clipboard with the given representations.
func (androidJNI) ClipboardWrite(p backend.ClipboardPayload) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	plain, plainLen := cBytes(p.Plain)
	defer cFree(plain)
	html, htmlLen := cBytes(p.HTML)
	defer cFree(html)
	rc := C.mygo_platform_write(
		boolToC(p.HasPlain), plain, plainLen,
		boolToC(p.HasHTML), html, htmlLen)
	if rc != 0 {
		return errClipboardWrite
	}
	return nil
}

// ClipboardClear empties the clipboard.
func (androidJNI) ClipboardClear() error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	if C.mygo_platform_clear() < 0 {
		return backend.ErrPlatformBridge
	}
	return nil
}

// OpenExternalURL hands the URL to the system.
func (androidJNI) OpenExternalURL(url string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	c, n := cBytes(url)
	defer cFree(c)
	switch rc := C.mygo_platform_open(c, n); rc {
	case 0:
		return nil
	case 1:
		// The Java side found no activity for the intent, which is what a
		// device with no browser (or no mail client) looks like.
		return errNoActivity
	default:
		return backend.ErrPlatformBridge
	}
}

// NotificationsAllowed reports whether the user allows notifications.
//
// Before Java attaches this is false: nothing can be posted, and false is the
// honest answer for "not now".
func (androidJNI) NotificationsAllowed() bool {
	return C.mygo_platform_notify_allowed() == 1
}

// NetworkStatus reports the connection: bit 0 a network the app can use, bit
// 1 that it is metered. A call that did not happen reads as offline.
func (androidJNI) NetworkStatus() (online, metered bool) {
	v := C.mygo_platform_network_status()
	return v&1 != 0, v&2 != 0
}

// Multicast holds the Wi-Fi multicast lock. 1 is the Java side saying the
// device has no Wi-Fi at all, which is unsupported rather than a failure.
func (androidJNI) Multicast(on bool) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	switch rc := C.mygo_platform_set_multicast(boolToC(on)); rc {
	case 0:
		return nil
	case 1:
		return platform.ErrUnsupported
	default:
		return backend.ErrPlatformBridge
	}
}

// StartForeground runs the app in a foreground service.
func (androidJNI) StartForeground(reason string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	r, rn := cBytes(reason)
	defer cFree(r)
	switch rc := C.mygo_platform_start_foreground(r, rn); rc {
	case 0:
		return nil
	case 1:
		return backend.ErrPlatformBridge
	default:
		return backend.ErrForegroundService
	}
}

// StopForeground stops it.
func (androidJNI) StopForeground() error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	if C.mygo_platform_stop_foreground() != 0 {
		return backend.ErrForegroundService
	}
	return nil
}

// StartBLEScan starts a Bluetooth LE scan; events are pulled with TakeBLEEvent.
func (androidJNI) StartBLEScan(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_ble_start(b, n))
}

// StopBLEScan ends the scan.
func (androidJNI) StopBLEScan() int {
	if !attached() {
		return 0
	}
	return int(C.mygo_platform_ble_stop())
}

// TakeBLEEvent pulls one queued scan event as JSON.
func (androidJNI) TakeBLEEvent() (string, bool) {
	if !attached() {
		return "", false
	}
	var n C.int
	return takeJavaBytes(C.mygo_platform_ble_take(&n), n)
}

// BLEConnect connects to the peripheral described by JSON; the result arrives
// as an event.
func (androidJNI) BLEConnect(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_ble_connect(b, n))
}

// BLEDisconnect drops the connection.
func (androidJNI) BLEDisconnect(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_ble_disconnect(b, n))
}

// BLERead asks for a characteristic value; it arrives as an event.
func (androidJNI) BLERead(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_ble_read(b, n))
}

// BLEWrite writes a characteristic value.
func (androidJNI) BLEWrite(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_ble_write(b, n))
}

// BLESubscribe turns notifications for a characteristic on or off.
func (androidJNI) BLESubscribe(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_ble_subscribe(b, n))
}

// BLEBond starts pairing with the device at the JSON address; the state
// arrives as an event.
func (androidJNI) BLEBond(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_ble_bond(b, n))
}

// BLERequestMTU asks for a larger packet size.
func (androidJNI) BLERequestMTU(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_ble_mtu(b, n))
}

// NFCAvailable reports whether the device has an NFC adapter.
func (androidJNI) NFCAvailable() bool {
	if !attached() {
		return false
	}
	return C.mygo_platform_nfc_available() != 0
}

// NFCScanStart enables the foreground reader.
func (androidJNI) NFCScanStart() int {
	if !attached() {
		return 3
	}
	return int(C.mygo_platform_nfc_start())
}

// NFCScanStop disables it.
func (androidJNI) NFCScanStop() int {
	if !attached() {
		return 3
	}
	return int(C.mygo_platform_nfc_stop())
}

// TakeNFCEvent pulls one queued tag as JSON.
func (androidJNI) TakeNFCEvent() (string, bool) {
	if !attached() {
		return "", false
	}
	var n C.int
	return takeJavaBytes(C.mygo_platform_nfc_take(&n), n)
}

// NFCWriteText writes a text record to the tag in the field.
func (androidJNI) NFCWriteText(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_nfc_write(b, n))
}

// NFCTransceive sends one raw command to the tag in the field.
func (androidJNI) NFCTransceive(json string) (string, bool) {
	if !attached() {
		return "", false
	}
	b, n := cBytes(json)
	defer cFree(b)
	var out C.int
	return takeJavaBytes(C.mygo_platform_nfc_transceive(b, n, &out), out)
}

// HostEvent hands one JSON message to the host.
func (androidJNI) HostEvent(json string) int {
	if !attached() {
		return -1
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_host_event(b, n))
}

// HCEStart turns on card emulation for the given AIDs.
func (androidJNI) HCEStart(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_hce_start(b, n))
}

// HCEStop turns it off.
func (androidJNI) HCEStop() int {
	if !attached() {
		return 3
	}
	return int(C.mygo_platform_hce_stop())
}

// TakeHCERequest pulls one waiting APDU as JSON.
func (androidJNI) TakeHCERequest() (string, bool) {
	if !attached() {
		return "", false
	}
	var n C.int
	return takeJavaBytes(C.mygo_platform_hce_take(&n), n)
}

// HCERespond hands the answer back to the waiting service.
func (androidJNI) HCERespond(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_hce_respond(b, n))
}

// AudioVolume reports the media volume and ringer mode as JSON.
func (androidJNI) AudioVolume() (string, bool) {
	if !attached() {
		return "", false
	}
	var n C.int
	return takeJavaBytes(C.mygo_platform_audio_volume(&n), n)
}

// AudioSetVolume sets the media volume, mutes it, or steps it.
func (androidJNI) AudioSetVolume(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_audio_set(b, n))
}

// AudioMediaKey sends one media key.
func (androidJNI) AudioMediaKey(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_audio_key(b, n))
}

// AudioRingerMode sets the ringer mode; 2 means it needs the silence access.
func (androidJNI) AudioRingerMode(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_audio_ringer(b, n))
}

// SilenceAllowed reports whether silent is permitted.
func (androidJNI) SilenceAllowed() bool {
	if !attached() {
		return false
	}
	return C.mygo_platform_silence_allowed() != 0
}

// OpenSilenceSettings shows the system page that grants it.
func (androidJNI) OpenSilenceSettings() int {
	if !attached() {
		return 3
	}
	return int(C.mygo_platform_silence_settings())
}

// WifiScan starts a scan for nearby networks; the list arrives as an event.
func (androidJNI) WifiScan() int {
	if !attached() {
		return 3
	}
	return int(C.mygo_platform_wifi_scan())
}

// TakeWifiEvent pulls one queued scan or join event as JSON.
func (androidJNI) TakeWifiEvent() (string, bool) {
	if !attached() {
		return "", false
	}
	var n C.int
	return takeJavaBytes(C.mygo_platform_wifi_take(&n), n)
}

// WifiJoin joins the network in json and routes the app through it.
func (androidJNI) WifiJoin(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_wifi_join(b, n))
}

// WifiLeave drops that network and restores the default route.
func (androidJNI) WifiLeave() int {
	if !attached() {
		return 3
	}
	return int(C.mygo_platform_wifi_leave())
}

// Speak hands the JSON {text, rate, pitch, flush} to the Java side.
func (androidJNI) Speak(json string) int {
	if !attached() {
		return 3
	}
	b, n := cBytes(json)
	defer cFree(b)
	return int(C.mygo_platform_speak(b, n))
}

// StopSpeaking stops whatever synthesis is playing.
func (androidJNI) StopSpeaking() int {
	if !attached() {
		return 0
	}
	return int(C.mygo_platform_stop_speaking())
}

// UpdateTile hands the quick settings tile's content to the Java side.
func (androidJNI) UpdateTile(json string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	b, n := cBytes(json)
	defer cFree(b)
	switch rc := C.mygo_platform_update_tile(b, n); rc {
	case 0:
		return nil
	case 1:
		return backend.ErrPlatformBridge
	default:
		return platform.ErrUnsupported
	}
}

// SystemProxy reads the proxy the system configured, as JSON.
func (androidJNI) SystemProxy() (string, int, bool) {
	if !attached() {
		return "", 0, false
	}
	var n C.int
	raw, ok := takeJavaBytes(C.mygo_platform_system_proxy(&n), n)
	if !ok || raw == "" || raw == "{}" {
		return "", 0, false
	}
	var info struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := json.Unmarshal([]byte(raw), &info); err != nil || info.Host == "" {
		return "", 0, false
	}
	return info.Host, info.Port, true
}

// SetSecureScreen adds or clears the window's secure flag.
func (androidJNI) SetSecureScreen(on bool) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	switch rc := C.mygo_platform_set_secure_screen(boolToC(on)); rc {
	case 0:
		return nil
	case 1:
		return backend.ErrPlatformBridge
	default:
		return platform.ErrUnsupported
	}
}

// BatteryExempt reports whether the system exempts the app from battery
// optimizations.
func (androidJNI) BatteryExempt() (bool, error) {
	if !attached() {
		return false, backend.ErrPlatformBridge
	}
	switch C.mygo_platform_battery_exempt() {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, backend.ErrPlatformBridge
	}
}

// RequestBatteryExemption shows the system's prompt, or its settings page when
// a ROM refuses the direct request.
func (androidJNI) RequestBatteryExemption() error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	switch C.mygo_platform_request_battery_exemption() {
	case 0, 1:
		return nil
	default:
		return backend.ErrPlatformBridge
	}
}

// UpdateWidget hands the widget content to the Java side.
func (androidJNI) UpdateWidget(json string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	b, n := cBytes(json)
	defer cFree(b)
	switch rc := C.mygo_platform_update_widget(b, n); rc {
	case 0:
		return nil
	case 1:
		return backend.ErrPlatformBridge
	default:
		return backend.ErrWidgetUpdate
	}
}

// Location reads the last fix as JSON; an empty object is no fix.
func (androidJNI) Location() (lat, lon, accuracy float64, atMillis int64, ok bool) {
	if !attached() {
		return 0, 0, 0, 0, false
	}
	var n C.int
	raw, got := takeJavaBytes(C.mygo_platform_location(&n), n)
	if !got || raw == "" || raw == "{}" {
		return 0, 0, 0, 0, false
	}
	var info struct {
		Lat      float64 `json:"lat"`
		Lon      float64 `json:"lon"`
		Accuracy float64 `json:"accuracy"`
		Time     int64   `json:"time"`
	}
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return 0, 0, 0, 0, false
	}
	return info.Lat, info.Lon, info.Accuracy, info.Time, true
}

// WifiInfo reads the current network as JSON: an SSID can carry any text,
// including the quotes the platform puts around it and characters JNI's
// modified UTF-8 would mangle.
func (androidJNI) WifiInfo() (string, string, bool) {
	if !attached() {
		return "", "", false
	}
	var n C.int
	raw, ok := takeJavaBytes(C.mygo_platform_wifi_info(&n), n)
	if !ok {
		return "", "", false
	}
	var info struct {
		SSID      string `json:"ssid"`
		BSSID     string `json:"bssid"`
		Connected bool   `json:"connected"`
	}
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return "", "", false
	}
	return info.SSID, info.BSSID, info.Connected
}

// PostNotification posts one notification.
func (androidJNI) PostNotification(n *platform.Notification) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	if n == nil {
		return errNilNotification
	}
	payload := notificationPayload{
		ID:    n.ID,
		Title: n.Title,
		// The subtitle has nowhere of its own to go on Android (a notification
		// has a title and a body), so it leads the body, the way the Linux
		// backend's D-Bus body does.
		Body:     joinSubtitle(n.Subtitle, n.Body),
		Silent:   n.Silent,
		Urgent:   n.Urgent,
		Progress: n.Progress,
		Delay:    n.DelaySeconds,
	}
	for _, a := range n.Actions {
		payload.Actions = append(payload.Actions, notificationActionPayload{ID: a.ID, Label: a.Label})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	body, bodyLen := cBytes(string(encoded))
	defer cFree(body)
	switch rc := C.mygo_platform_notify(body, bodyLen); rc {
	case 0:
		return nil
	case 1:
		return platform.ErrNotificationsDenied
	case 2:
		return errPostNotification
	default:
		return backend.ErrPlatformBridge
	}
}

// notificationPayload is one JSON object for postNotification: id, title, body,
// silent, urgent, progress and the action buttons. A positional signature would
// grow a parameter per feature and still could not carry a list.
type notificationPayload struct {
	ID       string                      `json:"id"`
	Title    string                      `json:"title"`
	Body     string                      `json:"body"`
	Silent   bool                        `json:"silent"`
	Urgent   bool                        `json:"urgent"`
	Progress *float64                    `json:"progress,omitempty"`
	Actions  []notificationActionPayload `json:"actions,omitempty"`
	// Delay is how long to wait before showing it. The host schedules it with
	// the system, so it still arrives when the process is gone.
	Delay float64 `json:"delay,omitempty"`
}

type notificationActionPayload struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// CancelNotification removes one notification.
func (androidJNI) CancelNotification(id string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	c, n := cBytes(id)
	defer cFree(c)
	if C.mygo_platform_cancel(c, n) < 0 {
		return backend.ErrPlatformBridge
	}
	return nil
}

// CancelAllNotifications removes the app's notifications.
func (androidJNI) CancelAllNotifications() error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	if C.mygo_platform_cancel_all() < 0 {
		return backend.ErrPlatformBridge
	}
	return nil
}

// AnnounceAs puts text through the platform's accessibility announcement.
func (androidJNI) AnnounceAs(text string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	c, n := cBytes(text)
	defer cFree(c)
	if C.mygo_platform_announce(c, n) < 0 {
		return backend.ErrPlatformBridge
	}
	return nil
}

// SetAccessibilityTreeJSON gives the Java side the tree its node provider
// serves.
func (androidJNI) SetAccessibilityTreeJSON(json string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	c, n := cBytes(json)
	defer cFree(c)
	if C.mygo_platform_set_tree(c, n) < 0 {
		return backend.ErrPlatformBridge
	}
	return nil
}

// SecretSet stores value under namespace/key in the Keystore.
func (androidJNI) SecretSet(namespace, key string, value []byte, accessibility string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	ns, nsLen := cBytes(namespace)
	defer cFree(ns)
	k, kLen := cBytes(key)
	defer cFree(k)
	v, vLen := cBytes(string(value))
	defer cFree(v)
	// 1 is "the Java side stored it already, or refused"; both are final.
	switch C.mygo_platform_secret_set(ns, nsLen, k, kLen, v, vLen, C.int(accessibilityCode(accessibility))) {
	case -1:
		return backend.ErrPlatformBridge
	case 0:
		return nil
	}
	return errSecretStore
}

// SecretGet reads value. A missing one is platform.ErrSecretNotFound.
func (androidJNI) SecretGet(namespace, key string) ([]byte, error) {
	if !attached() {
		return nil, backend.ErrPlatformBridge
	}
	ns, nsLen := cBytes(namespace)
	defer cFree(ns)
	k, kLen := cBytes(key)
	defer cFree(k)
	var n C.int
	s, ok := takeJavaBytes(C.mygo_platform_secret_get(ns, nsLen, k, kLen, &n), n)
	if !ok {
		// The Java side returns null for both "no such secret" and a failed
		// unwrap; attaching is what tells them apart here, and the platform
		// is attached by the check above, so it is the store's answer.
		return nil, platform.ErrSecretNotFound
	}
	return []byte(s), nil
}

// SecretDelete removes value; removing one that is not there is not an error.
func (androidJNI) SecretDelete(namespace, key string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	ns, nsLen := cBytes(namespace)
	defer cFree(ns)
	k, kLen := cBytes(key)
	defer cFree(k)
	switch C.mygo_platform_secret_delete(ns, nsLen, k, kLen) {
	case -1:
		return backend.ErrPlatformBridge
	case 0:
		return nil
	}
	return errSecretStore
}

// ScanCode opens the system's image picker through the bridge.
func (androidJNI) ScanCode(token uint64, prompt string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	p, n := cBytes(prompt)
	defer cFree(p)
	switch C.mygo_platform_scan(C.jlong(token), p, n) {
	case -1:
		return backend.ErrPlatformBridge
	case 0:
		go collectScanResults()
		return nil
	}
	return backend.ErrScanUnavailable
}

// PickPhotos opens the system photo picker through the bridge.
func (androidJNI) PickPhotos(token uint64, multiple bool) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	var m C.jboolean
	if multiple {
		m = 1
	}
	switch C.mygo_platform_pick_photos(C.jlong(token), m) {
	case -1:
		return backend.ErrPlatformBridge
	case 0:
		go collectMediaResults()
		return nil
	}
	return backend.ErrPickerUnavailable
}

// ExportFile opens the system document picker for the file at path.
func (androidJNI) ExportFile(token uint64, path, title string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	p, pn := cBytes(path)
	defer cFree(p)
	t, tn := cBytes(title)
	defer cFree(t)
	switch C.mygo_platform_export_file(C.jlong(token), p, pn, t, tn) {
	case -1:
		return backend.ErrPlatformBridge
	case 0:
		go collectMediaResults()
		return nil
	}
	return backend.ErrPickerUnavailable
}

// RegisterPush starts FCM registration through the bridge.
func (androidJNI) RegisterPush() error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	switch C.mygo_platform_register_push() {
	case -1:
		return backend.ErrPlatformBridge
	case 0:
		go collectMediaResults()
		return nil
	}
	return platform.ErrUnsupported
}

// PermissionStatus reads the grant the system recorded, without prompting:
// 0 granted, 1 denied, 2 not determined. A bridge that is not there answers 3,
// which the Go side reports as unsupported rather than guessing.
func (androidJNI) PermissionStatus(kind int) int {
	rc := int(C.mygo_platform_permission_status(C.jint(kind)))
	if rc < 0 {
		return 3
	}
	return rc
}

// RequestPermission asks for kind and starts collecting the answer.
func (androidJNI) RequestPermission(kind int, token uint64) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	switch C.mygo_platform_request_permission(C.jint(kind), C.jlong(token)) {
	case -1:
		return backend.ErrPlatformBridge
	case 0:
		go collectPermissionResults()
		return nil
	}
	return platform.ErrUnsupported
}

// OpenAppSettings opens this app's page in the system settings.
func (androidJNI) OpenAppSettings() error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	switch C.mygo_platform_open_settings() {
	case -1:
		return backend.ErrPlatformBridge
	case 0:
		return nil
	}
	return errors.New("mygo: Android could not open the app's settings")
}

// KeepAwake adds or removes one reason for waking, through the bridge.
func (androidJNI) KeepAwake(display bool, reason string, on bool) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	r, rn := cBytes(reason)
	defer cFree(r)
	var displayFlag, onFlag C.jboolean
	if display {
		displayFlag = 1
	}
	if on {
		onFlag = 1
	}
	switch C.mygo_platform_keep_awake(displayFlag, r, rn, onFlag) {
	case -1:
		return backend.ErrPlatformBridge
	case 0:
		return nil
	}
	return errors.New("mygo: Android could not change the wake state")
}

// OnBattery reports whether the device runs on its battery.
func (androidJNI) OnBattery() bool { return C.mygo_platform_on_battery() == 1 }

// DeviceInfo reads the phone's identity the Java side reported.
func (androidJNI) DeviceInfo() (platform.DeviceInfo, error) {
	if !attached() {
		return platform.DeviceInfo{}, backend.ErrPlatformBridge
	}
	var n C.int
	raw, ok := takeJavaBytes(C.mygo_platform_device_info(&n), n)
	if !ok {
		return platform.DeviceInfo{}, errors.New("mygo: Android reported no device information")
	}
	var wire struct {
		Name    string `json:"name"`
		System  string `json:"system"`
		Version string `json:"version"`
		Model   string `json:"model"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return platform.DeviceInfo{}, err
	}
	return platform.DeviceInfo{Name: wire.Name, System: wire.System, Version: wire.Version, Model: wire.Model}, nil
}

// SpeechStart begins recognition and starts collecting its answers.
func (androidJNI) SpeechStart(token uint64, language string, onDevice bool, contextual []string) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	lang, langLen := cBytes(language)
	defer cFree(lang)
	hints, hintsLen := cBytes(strings.Join(contextual, "\n"))
	defer cFree(hints)
	var device C.jboolean
	if onDevice {
		device = 1
	}
	switch C.mygo_platform_speech_start(C.jlong(token), lang, langLen, device, hints, hintsLen) {
	case -1:
		return backend.ErrPlatformBridge
	case 0:
		go collectSpeechResults(token)
		return nil
	case 1:
		return platform.ErrUnsupported
	}
	return errors.New("mygo: Android could not start speech recognition")
}

// SpeechStop asks for the final result.
func (androidJNI) SpeechStop(token uint64) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	if C.mygo_platform_speech_stop(C.jlong(token)) < 0 {
		return backend.ErrPlatformBridge
	}
	return nil
}

// SpeechCancel discards the session.
func (androidJNI) SpeechCancel(token uint64) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	if C.mygo_platform_speech_cancel(C.jlong(token)) < 0 {
		return backend.ErrPlatformBridge
	}
	return nil
}

// collectSpeechResults polls for recognizer answers. A session ends at its
// final result or its error, and a cancellation leaves it to the deadline.
func collectSpeechResults(want uint64) {
	const interval = 100 * time.Millisecond
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		token, code, text, ok := takeSpeechResult()
		if !ok {
			time.Sleep(interval)
			continue
		}
		backend.SpeechResult(token, code, text)
		if code != 0 {
			return
		}
	}
	// The session layer ends a capture at its own duration and cancels it, so
	// no final answer is coming; free the entry rather than keeping a callback
	// for the life of the process.
	backend.SpeechTimedOut(want)
}

// takeSpeechResult pulls one recognizer answer. The encoding is
// "<token>\n<code>\n<text>", and the text may itself hold newlines, so only
// the first two separators count.
func takeSpeechResult() (token uint64, code int, text string, ok bool) {
	raw := C.mygo_platform_take_speech_result()
	if raw == nil {
		return 0, 0, "", false
	}
	defer C.free(unsafe.Pointer(raw))
	parts := strings.SplitN(C.GoString(raw), "\n", 3)
	if len(parts) < 2 {
		return 0, 0, "", false
	}
	n, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0, "", false
	}
	c, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil {
		return 0, 0, "", false
	}
	if len(parts) == 3 {
		text = parts[2]
	}
	return n, int(c), text, true
}

// collectPermissionResults polls for one permission answer. A prompt can stay
// up for minutes while the person decides, so the deadline matches the
// backend's; whichever collector takes the answer delivers it by token.
func collectPermissionResults() {
	const interval = 150 * time.Millisecond
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		if token, status, ok := takePermissionResult(); ok {
			backend.PermissionResult(token, status)
			return
		}
		time.Sleep(interval)
	}
}

// takePermissionResult pulls one pending answer. The encoding is
// "<token>\n<status>", token first so a malformed tail cannot be mistaken for
// another request's.
func takePermissionResult() (token uint64, status int, ok bool) {
	raw := C.mygo_platform_take_permission_result()
	if raw == nil {
		return 0, 0, false
	}
	defer C.free(unsafe.Pointer(raw))
	parts := strings.SplitN(C.GoString(raw), "\n", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	n, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	s, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil {
		return 0, 0, false
	}
	return n, int(s), true
}

// collectMediaResults polls for a photo or export answer. The wait is a person
// browsing a picker, so it is generous; one getString every 150ms is nothing.
func collectMediaResults() {
	const interval = 150 * time.Millisecond
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		if kind, token, payload, ok := takeMediaResult(); ok {
			switch kind {
			case 0:
				var paths []string
				if payload != "" {
					paths = strings.Split(payload, "\n")
				}
				backend.PhotoResult(token, paths)
			case 1:
				backend.ExportResult(token, payload == "1")
			case 2:
				backend.PushRegistered(payload, nil)
			case 3:
				var perr error
				if payload != "" {
					perr = errors.New(payload)
				}
				backend.PushRegistered("", perr)
			}
			return
		}
		time.Sleep(interval)
	}
}

// takeMediaResult pulls one pending answer. The encoding is
// "<kind>\n<token>\n<payload>": 0 photos with newline-joined paths, 1 export
// with "1" completed or "0" cancelled.
func takeMediaResult() (kind int32, token uint64, payload string, ok bool) {
	raw := C.mygo_platform_take_media_result()
	if raw == nil {
		return 0, 0, "", false
	}
	defer C.free(unsafe.Pointer(raw))
	parts := strings.SplitN(C.GoString(raw), "\n", 3)
	if len(parts) < 2 {
		return 0, 0, "", false
	}
	k, err := strconv.ParseInt(parts[0], 10, 32)
	if err != nil {
		return 0, 0, "", false
	}
	n, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, 0, "", false
	}
	if len(parts) == 3 {
		payload = parts[2]
	}
	return int32(k), n, payload, true
}

// collectScanResults polls the Java side until it hands over an answer.
//
// The bridge is one-way — Go calls Java through jmethodID, and nothing on the
// Java side calls back into Go — so a scan cannot arrive as a JNI entry point.
// Polling is the shape that fits: the wait is a person holding a camera or
// picking a photo (seconds), and one getString every 150ms costs nothing.
// It stops as soon as an answer is taken, or after a minute, which is longer
// than anyone leaves a camera up.
func collectScanResults() {
	const interval = 150 * time.Millisecond
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if code, token, text, ok := takeScanResult(); ok {
			backend.ScanResult(token, text, code)
			return
		}
		time.Sleep(interval)
	}
}

// takeScanResult pulls one pending result from the Java side, if there is one.
// The encoding is "<code>\n<token>\n<text>": code first so a malformed tail
// still says whether the scan failed as canceled or as a decode miss.
func takeScanResult() (code int32, token uint64, text string, ok bool) {
	raw := C.mygo_platform_take_scan_result()
	if raw == nil {
		return 0, 0, "", false
	}
	defer C.free(unsafe.Pointer(raw))
	parts := strings.SplitN(C.GoString(raw), "\n", 3)
	if len(parts) < 2 {
		return 0, 0, "", false
	}
	code64, err := strconv.ParseInt(parts[0], 10, 32)
	if err != nil {
		return 0, 0, "", false
	}
	n, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, 0, "", false
	}
	if len(parts) == 3 {
		text = parts[2]
	}
	return int32(code64), n, text, true
}

// ScanCancel takes the picker down, if it is still up.
func (androidJNI) ScanCancel(token uint64) error {
	if !attached() {
		return backend.ErrPlatformBridge
	}
	if C.mygo_platform_scan_cancel(C.jlong(token)) < 0 {
		return backend.ErrPlatformBridge
	}
	return nil
}

// accessibilityCode maps mygo's accessibility to the int the Java side takes.
// The Keystore has no per-item lifetime — an entry is readable whenever the
// device is unlocked — so only the after-first-unlock value has a meaning here
// (readable from the first unlock after boot, which is what the Java side
// implements by never holding a key that needs the screen on).
func accessibilityCode(s string) int {
	if s == "after-first-unlock" {
		return 1
	}
	return 0
}

// The errors this file adds. None of them is platform.ErrNotificationsDenied,
// which says the user said no: these say the call or the platform failed.
var (
	errClipboardWrite   = errors.New("mygo: Android refused the clipboard write")
	errNoActivity       = errors.New("mygo: no app on this device handles that URL")
	errNilNotification  = errors.New("mygo: nil notification")
	errPostNotification = errors.New("mygo: Android refused the notification")
	errSecretStore      = errors.New("mygo: Android refused the secure store write")
)

// joinSubtitle is the body a notification shows when it carries a subtitle.
func joinSubtitle(subtitle, body string) string {
	if subtitle == "" {
		return body
	}
	if body == "" {
		return subtitle
	}
	return subtitle + "\n" + body
}

// takeJavaBytes copies what a C wrapper returned into a Go string. ok is false
// when the Java method reported no value (an absent clipboard representation,
// say), which is not the same as an empty one: mygo_platform_grab returns a
// 1-byte buffer for an empty array precisely so that the two stay apart.
func takeJavaBytes(p *C.char, n C.int) (string, bool) {
	if p == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoStringN(p, n), true
}

// cBytes makes a malloc'd copy of s for the C side, with its length. The pointer
// is nil for the empty string — the C side treats an empty argument as present
// and empty (mygo_platform_arg), so nothing is lost — and every caller frees it.
func cBytes(s string) (*C.char, C.int) {
	if s == "" {
		return nil, 0
	}
	return (*C.char)(C.CBytes([]byte(s))), C.int(len(s))
}

// cFree releases what cBytes allocated.
func cFree(p *C.char) {
	if p != nil {
		C.free(unsafe.Pointer(p))
	}
}

// boolToC turns a Go bool into the int the C side takes.
func boolToC(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

// javaBytes copies the bytes of a byte[] argument of a //export function into a
// Go string. A null array is the empty string: a value the Java side sends is
// always an array (empty when the value is empty), so null means the caller
// passed nothing at all.
func javaBytes(env *C.JNIEnv, data C.jbyteArray) string {
	var buf *C.char
	n := C.mygo_platform_java_bytes(env, data, &buf)
	if buf == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(buf))
	return C.GoStringN(buf, n)
}

// The //export functions below are what Java calls into this library
// (org.mygo.MygoPlatform's external methods). Their names are the JNI symbols
// Java binds to, so they are spelled out by MygoPlatform.kt and must not
// change: Java_org_mygo_MygoPlatform_<method>.
//
// MygoPlatform is a Kotlin object, so its external members are *instance*
// methods of the singleton class: the second argument is the object, not a
// jclass. That is what keeps the symbol exactly Java_org_mygo_MygoPlatform_x —
// a companion object with @JvmStatic would have moved the native method to
// MygoPlatform$Companion and renamed the symbol with it.

// Java_org_mygo_MygoPlatform_nativeAttach is MygoPlatform.nativeAttach: the
// Activity's install() calls it once, and it is what makes the JavaVM and the
// MygoPlatform object reachable from Go.
//
//export Java_org_mygo_MygoPlatform_nativeAttach
func Java_org_mygo_MygoPlatform_nativeAttach(env *C.JNIEnv, thiz C.jobject) {
	rc := C.mygo_platform_attach(env, thiz)
	switch rc {
	case 0:
		androidLog("MygoPlatform.attach ok")
	case -2:
		// A method of MygoPlatform was renamed: the platform features are
		// off until both sides are in step. This is the one failure the Java
		// side cannot see coming, so it is loud.
		androidLog("MygoPlatform.attach FAILED: a method this library calls is missing (renamed?)")
	default:
		androidLog("MygoPlatform.attach failed rc=%d", int(rc))
	}
}

// Java_org_mygo_MygoPlatform_nativeNotificationClicked is
// MygoPlatform.nativeNotificationClicked: the user tapped a notification, and
// id is the identifier the app gave it.
//
//export Java_org_mygo_MygoPlatform_nativeOpenURL
func Java_org_mygo_MygoPlatform_nativeOpenURL(env *C.JNIEnv, thiz C.jobject, url C.jbyteArray) {
	raw := javaBytes(env, url)
	if !backend.DispatchOpenURL(raw) {
		// Buffered until Init installs the sink, or dropped when the buffer is
		// full; either way logcat says which, instead of the link vanishing.
		androidLog("mygo/android: URL held before the app was ready: %s", raw)
	}
}

// Java_org_mygo_MygoPlatform_nativePushMessage is nativePushMessage: a
// data-only FCM message, forwarded to the app handler as JSON while the app
// runs. There is nothing to update when it does not, so it is dropped.
//
// Java_org_mygo_MygoPlatform_nativeTileClick is nativeTileClick: the quick
// settings tile was tapped.
//
//export Java_org_mygo_MygoPlatform_nativeTileClick
func Java_org_mygo_MygoPlatform_nativeTileClick(env *C.JNIEnv, thiz C.jobject) {
	if !backend.DispatchTileClick() {
		androidLog("quick settings tile tapped with no tile sink installed")
	}
}

// Java_org_mygo_MygoPlatform_nativeAccessibilityOn is nativeAccessibilityOn:
// an accessibility service is on, so the ui layer starts describing frames.
//
//export Java_org_mygo_MygoPlatform_nativeAccessibilityOn
func Java_org_mygo_MygoPlatform_nativeAccessibilityOn(env *C.JNIEnv, thiz C.jobject) {
	theBackend.NotifyAccessibility()
}

// Java_org_mygo_MygoPlatform_nativeWidgetAction is nativeWidgetAction: a row
// of the home screen widget was tapped.
//
//export Java_org_mygo_MygoPlatform_nativeWidgetAction
func Java_org_mygo_MygoPlatform_nativeWidgetAction(env *C.JNIEnv, thiz C.jobject, id C.jbyteArray) {
	if !backend.DispatchWidgetAction(javaBytes(env, id)) {
		androidLog("widget row tapped with no widget sink installed")
	}
}

// Java_org_mygo_MygoPlatform_nativeNotificationAction is
// nativeNotificationAction: a button on a notification was pressed.
//
//export Java_org_mygo_MygoPlatform_nativeNotificationAction
func Java_org_mygo_MygoPlatform_nativeNotificationAction(env *C.JNIEnv, thiz C.jobject, id C.jbyteArray, action C.jbyteArray) {
	if !backend.DispatchNotificationAction(javaBytes(env, id), javaBytes(env, action)) {
		androidLog("notification action with no action sink installed")
	}
}

//export Java_org_mygo_MygoPlatform_nativePushMessage
func Java_org_mygo_MygoPlatform_nativePushMessage(env *C.JNIEnv, thiz C.jobject, json C.jbyteArray) {
	backend.PushMessage(javaBytes(env, json))
}

//export Java_org_mygo_MygoPlatform_nativeNotificationClicked
func Java_org_mygo_MygoPlatform_nativeNotificationClicked(env *C.JNIEnv, thiz C.jobject, id C.jbyteArray) {
	clicked := javaBytes(env, id)
	if !backend.DispatchNotificationClicked(clicked) {
		// No sink: the tap is not lost silently, it is reported as
		// undelivered — the state of the wiring, visible in logcat.
		androidLog("notification %q tapped with no click sink installed", clicked)
	}
}

// Java_org_mygo_MygoPlatform_nativeAccessibilityAction is
// MygoPlatform.nativeAccessibilityAction: TalkBack performed an action on the
// element id, named as the Go side names it ("press", "focus", "setvalue").
//
//export Java_org_mygo_MygoPlatform_nativeAccessibilityAction
func Java_org_mygo_MygoPlatform_nativeAccessibilityAction(env *C.JNIEnv, thiz C.jobject, id C.jlong, action C.jbyteArray) {
	name := javaBytes(env, action)
	kind, ok := backend.ParseAccessActionKind(name)
	if !ok {
		androidLog("accessibility: unknown action %q on element %d", name, int64(id))
		return
	}
	if !backend.DispatchAccessAction(uint64(id), kind) {
		androidLog("accessibility: %s on element %d with no action sink installed", name, int64(id))
	}
}
