// shelf_np_helper - a small C bundle that reads the system Now Playing
// state through MediaRemote.framework and streams it as JSON lines on
// stdout, taking simple commands on stdin.
//
// It is NOT run on its own: since macOS 15.4 MediaRemote only answers
// clients whose code is Apple-signed, so this bundle is loaded into an
// Apple-signed host (/usr/bin/perl) by the Shelf app; the host installs
// shelf_np_run as an xsub and calls it. Everything runs in the host
// process; the bundle never spawns one.
//
// Protocol, one JSON object per line on stdout:
//   {"ok":true,"playing":<bool>,"title":..,"artist":..,"album":..,
//    "duration":<sec>,"elapsed":<sec>,"at":<unix time>,
//    "app":<bundle id>,"artID":<track id>,"art":<base64 png>,
//    "artMissing":<bool>}
// art is sent once per new track (artID change); artMissing marks a track
// whose artwork could not be read so the reader does not wait for it.
// Commands, one per line on stdin:
//   play | pause | toggle | next | prev | stop | quit
// An unknown command gets {"error":"bad command"} back. EOF on stdin is
// the shutdown request: the process exits after the current read.

#include <CoreFoundation/CoreFoundation.h>
#include <dispatch/dispatch.h>
#include <dlfcn.h>
#include <objc/message.h>
#include <objc/objc.h>
#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

// ---- MediaRemote symbols ------------------------------------------------

typedef void (*GetInfoFn)(dispatch_queue_t, void (^)(CFDictionaryRef));
typedef void (*GetPlayingFn)(dispatch_queue_t, void (^)(Boolean));
typedef void (*RegFn)(dispatch_queue_t);
typedef void (*SendCommandFn)(uint32_t, CFDictionaryRef);
typedef void (*SetInfoFn)(CFDictionaryRef);
typedef void (*SetCanBeFn)(Boolean);

static void *MR;
static GetInfoFn MRGetInfo;
static GetPlayingFn MRGetIsPlaying;
static RegFn MRRegister;
static SendCommandFn MRSendCommand;

// MediaRemote command numbers, as in MPRemoteCommand (kMRMediaRemoteCommand).
enum {
  CMD_PLAY = 0,
  CMD_PAUSE = 1,
  CMD_TOGGLE = 2,
  CMD_STOP = 3,
  CMD_NEXT = 4,
  CMD_PREV = 5,
};

// Metadata keys, resolved from the framework (they are exported constants).
static CFStringRef KeyTitle, KeyArtist, KeyAlbum, KeyDuration, KeyElapsed,
    KeyRate, KeyArtwork, KeyItemID;

static char *cstr(CFStringRef s) {
  if (!s) return NULL;
  CFIndex n = CFStringGetMaximumSizeForEncoding(CFStringGetLength(s),
                                                kCFStringEncodingUTF8) + 1;
  char *buf = malloc((size_t)n);
  if (!buf) return NULL;
  if (!CFStringGetCString(s, buf, n, kCFStringEncodingUTF8)) {
    free(buf);
    return NULL;
  }
  return buf;
}

static int num(const void *v, double *out) {
  if (!v || CFGetTypeID(v) != CFNumberGetTypeID()) return 0;
  return CFNumberGetValue((CFNumberRef)v, kCFNumberDoubleType, out);
}

static CFStringRef strval(CFDictionaryRef d, CFStringRef key) {
  if (!d || !key) return NULL;
  const void *v = CFDictionaryGetValue(d, key);
  if (!v || CFGetTypeID(v) != CFStringGetTypeID()) return NULL;
  return (CFStringRef)v;
}

// ---- state --------------------------------------------------------------

typedef struct {
  int playing;
  char *title, *artist, *album, *app, *artID;
  double duration, elapsed; // elapsed is seconds at time `at`
  double rate;
  double at; // unix time the snapshot was written
  char *art; // base64 PNG of the current sample, NULL when absent
  long artLen;
  int artMissing; // artwork lookup failed for this track
} Snapshot;

static pthread_mutex_t emitLock = PTHREAD_MUTEX_INITIALIZER;
static Snapshot cur;     // what the reader last saw
static Snapshot pending; // latest read from the system
static char *sentArtID;  // track whose artwork was already emitted
static double lastEmit;  // unix time of the last emitted line
static int firstPublish = 1; // the reader always gets one line at startup
static double lastSample; // unix time of the last MediaRemote read
static volatile sig_atomic_t gotNotify;

static double nowUnix(void) {
  struct timeval tv;
  gettimeofday(&tv, NULL);
  return (double)tv.tv_sec + (double)tv.tv_usec / 1e6;
}

static void freeSnap(Snapshot *s) {
  free(s->title); free(s->artist); free(s->album); free(s->app);
  free(s->artID); free(s->art);
  memset(s, 0, sizeof *s);
}

static void setStr(char **dst, const char *src) {
  free(*dst);
  *dst = src ? strdup(src) : NULL;
}

// ---- JSON output --------------------------------------------------------

static void putEscaped(FILE *f, const char *s) {
  fputc('"', f);
  for (; s && *s; s++) {
    unsigned char c = (unsigned char)*s;
    switch (c) {
      case '"': fputs("\\\"", f); break;
      case '\\': fputs("\\\\", f); break;
      case '\n': fputs("\\n", f); break;
      case '\r': fputs("\\r", f); break;
      case '\t': fputs("\\t", f); break;
      default:
        if (c < 0x20)
          fprintf(f, "\\u%04x", c);
        else
          fputc(c, f);
    }
  }
  fputc('"', f);
}

static const char b64[] =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

static void emitSnapshot(const Snapshot *s, int withArt) {
  pthread_mutex_lock(&emitLock);
  FILE *f = stdout;
  fputs("{\"ok\":true,\"playing\":", f);
  fputs(s->playing ? "true" : "false", f);
  fputs(",\"title\":", f); putEscaped(f, s->title ? s->title : "");
  fputs(",\"artist\":", f); putEscaped(f, s->artist ? s->artist : "");
  fputs(",\"album\":", f); putEscaped(f, s->album ? s->album : "");
  fprintf(f, ",\"duration\":%.3f,\"elapsed\":%.3f,\"rate\":%.3f,\"at\":%.3f",
          s->duration, s->elapsed, s->rate, s->at);
  fputs(",\"app\":", f); putEscaped(f, s->app ? s->app : "");
  fputs(",\"artID\":", f); putEscaped(f, s->artID ? s->artID : "");
  if (s->artMissing) fputs(",\"artMissing\":true", f);
  if (withArt && s->art) {
    fputs(",\"art\":\"", f);
    fwrite(s->art, 1, (size_t)s->artLen, f);
    fputc('"', f);
  }
  fputs("}\n", f);
  fflush(f);
  pthread_mutex_unlock(&emitLock);
}

static void emitError(const char *msg) {
  pthread_mutex_lock(&emitLock);
  fputs("{\"error\":", stdout);
  putEscaped(stdout, msg ? msg : "error");
  fputs("}\n", stdout);
  fflush(stdout);
  pthread_mutex_unlock(&emitLock);
}

// ---- reading the system -------------------------------------------------

static dispatch_queue_t workQ;
static dispatch_semaphore_t workSem;

// readInfo fetches the dictionary and, when wantArt, the artwork blob.
// Returns 1 on success (dict may still be empty), 0 when nothing is
// playing / MediaRemote returned NULL.
static CFDictionaryRef readInfo(void) {
  __block CFDictionaryRef result = NULL;
  MRGetInfo(workQ, ^(CFDictionaryRef info) {
    if (info) result = CFRetain(info);
    dispatch_semaphore_signal(workSem);
  });
  if (dispatch_semaphore_wait(
          workSem, dispatch_time(DISPATCH_TIME_NOW, 3LL * NSEC_PER_SEC)) != 0)
    return NULL;
  return result;
}

static int readIsPlaying(void) {
  __block Boolean playing = false;
  MRGetIsPlaying(workQ, ^(Boolean p) {
    playing = p;
    dispatch_semaphore_signal(workSem);
  });
  if (dispatch_semaphore_wait(
          workSem, dispatch_time(DISPATCH_TIME_NOW, 3LL * NSEC_PER_SEC)) != 0)
    return -1;
  return playing ? 1 : 0;
}

static char *base64Copy(const unsigned char *data, long n) {
  long out = 4 * ((n + 2) / 3);
  char *s = malloc((size_t)out + 1);
  if (!s) return NULL;
  long o = 0;
  for (long i = 0; i < n; i += 3) {
    unsigned v = data[i] << 16;
    if (i + 1 < n) v |= data[i + 1] << 8;
    if (i + 2 < n) v |= data[i + 2];
    s[o++] = b64[(v >> 18) & 63];
    s[o++] = b64[(v >> 12) & 63];
    s[o++] = (i + 1 < n) ? b64[(v >> 6) & 63] : '=';
    s[o++] = (i + 2 < n) ? b64[v & 63] : '=';
  }
  s[o] = 0;
  return s;
}

// snapshot copies the current system state into s (free with freeSnap).
static void snapshot(Snapshot *s) {
  memset(s, 0, sizeof *s);
  s->at = nowUnix();
  s->playing = readIsPlaying() == 1;
  CFDictionaryRef info = readInfo();
  if (!info) return;
  char *t = cstr(strval(info, KeyTitle));
  setStr(&s->title, t);
  free(t);
  t = cstr(strval(info, KeyArtist));
  setStr(&s->artist, t);
  free(t);
  t = cstr(strval(info, KeyAlbum));
  setStr(&s->album, t);
  free(t);
  num(CFDictionaryGetValue(info, KeyDuration), &s->duration);
  num(CFDictionaryGetValue(info, KeyElapsed), &s->elapsed);
  if (!num(CFDictionaryGetValue(info, KeyRate), &s->rate)) s->rate = s->playing ? 1 : 0;
  CFStringRef item = strval(info, KeyItemID);
  t = cstr(item);
  setStr(&s->artID, t);
  free(t);
  if (!s->artID) {
    // No content item id: fall back to title+artist so a track change is
    // still visible to the reader.
    char buf[512];
    snprintf(buf, sizeof buf, "%s\n%s", s->title ? s->title : "", s->artist ? s->artist : "");
    setStr(&s->artID, buf);
  }
  // artwork
  const void *art = CFDictionaryGetValue(info, KeyArtwork);
  if (art && CFGetTypeID(art) == CFDataGetTypeID()) {
    CFDataRef d = (CFDataRef)art;
    CFIndex n = CFDataGetLength(d);
    if (n > 0 && n < 8 * 1024 * 1024) {
      const UInt8 *bytes = CFDataGetBytePtr(d);
      s->art = base64Copy(bytes, (long)n);
      s->artLen = s->art ? (long)strlen(s->art) : 0;
    }
  } else {
    s->artMissing = 1;
  }
  CFRelease(info);
}

// ---- change detection and emission --------------------------------------

static int stringsDiffer(const char *a, const char *b) {
  if (!a && !b) return 0;
  if (!a || !b) return 1;
  return strcmp(a, b) != 0;
}

// publish compares the pending sample with what the reader last saw,
// updates that baseline and prints whatever the reader needs to see.
// Only the polling thread calls it.
static void publish(void) {
  double now = nowUnix();
  int trackChanged = stringsDiffer(cur.artID, pending.artID);
  // The artwork goes out exactly once per track, when the first sample
  // that carries it arrives.
  int withArt = pending.art && stringsDiffer(sentArtID, pending.artID);
  int materialChange =
      cur.playing != pending.playing || trackChanged || withArt ||
      cur.artMissing != pending.artMissing ||
      stringsDiffer(cur.title, pending.title) ||
      stringsDiffer(cur.artist, pending.artist) ||
      stringsDiffer(cur.album, pending.album) ||
      stringsDiffer(cur.app, pending.app) ||
      (pending.duration > 0 ? cur.duration != pending.duration : 0) ||
      cur.rate != pending.rate;

  // Emit on change, or once a second while playing so the reader has a
  // fresh anchor to extrapolate the progress from. A paused or idle
  // system stays silent — except for the very first line, which tells
  // the reader whether anything is playing at all.
  int anchor = pending.playing && now - lastEmit >= 1.0;
  if (!materialChange && !anchor && !firstPublish) return;
  firstPublish = 0;

  if (withArt) {
    setStr(&sentArtID, pending.artID);
    emitSnapshot(&pending, 1);
  } else {
    emitSnapshot(&pending, 0);
  }
  freeSnap(&cur);
  cur = pending; // struct copy; ownership of the strings moves
  memset(&pending, 0, sizeof pending);
  lastEmit = now;
}

// poll is the periodic read (and the notification-triggered one).
static void poll(void) {
  lastSample = nowUnix();
  snapshot(&pending);
  publish();
}

// ---- notifications ------------------------------------------------------

static void onNotification(CFNotificationCenterRef center, void *observer,
                           CFNotificationName name, const void *object,
                           CFDictionaryRef userInfo) {
  (void)center; (void)observer; (void)name; (void)object; (void)userInfo;
  gotNotify = 1;
}

static void observeNotifications(void) {
  const char *names[] = {
      "kMRMediaRemoteNowPlayingInfoDidChangeNotification",
      "kMRMediaRemoteNowPlayingApplicationIsPlayingDidChangeNotification",
      "kMRMediaRemoteNowPlayingApplicationDidChangeNotification",
      NULL};
  CFNotificationCenterRef centers[] = {
      CFNotificationCenterGetLocalCenter(),
      CFNotificationCenterGetDistributedCenter()};
  for (int ci = 0; ci < 2; ci++) {
    for (int i = 0; names[i]; i++) {
      void **sym = dlsym(MR, names[i]);
      if (!sym || !*sym) continue;
      CFNotificationCenterAddObserver(centers[ci], NULL, onNotification,
                                      (CFStringRef)*sym, NULL,
                                      CFNotificationSuspensionBehaviorDeliverImmediately);
    }
  }
}

// ---- commands -----------------------------------------------------------

static void handleCommand(const char *line, int *quit) {
  unsigned cmd;
  if (!strcmp(line, "play")) cmd = CMD_PLAY;
  else if (!strcmp(line, "pause")) cmd = CMD_PAUSE;
  else if (!strcmp(line, "toggle")) cmd = CMD_TOGGLE;
  else if (!strcmp(line, "stop")) cmd = CMD_STOP;
  else if (!strcmp(line, "next")) cmd = CMD_NEXT;
  else if (!strcmp(line, "prev")) cmd = CMD_PREV;
  else if (!strcmp(line, "quit")) { *quit = 1; return; }
  else { emitError("bad command"); return; }
  MRSendCommand(cmd, NULL);
  // The system will tell us the new state; poll soon so it lands fast.
  gotNotify = 1;
}

// ---- entry --------------------------------------------------------------

static void loadMediaRemote(void) {
  MR = dlopen("/System/Library/PrivateFrameworks/MediaRemote.framework/MediaRemote",
              RTLD_NOW);
  if (!MR) {
    fprintf(stderr, "shelf-np: cannot load MediaRemote\n");
    exit(2);
  }
  MRGetInfo = (GetInfoFn)dlsym(MR, "MRMediaRemoteGetNowPlayingInfo");
  MRGetIsPlaying =
      (GetPlayingFn)dlsym(MR, "MRMediaRemoteGetNowPlayingApplicationIsPlaying");
  MRRegister =
      (RegFn)dlsym(MR, "MRMediaRemoteRegisterForNowPlayingNotifications");
  MRSendCommand = (SendCommandFn)dlsym(MR, "MRMediaRemoteSendCommand");
  KeyTitle = *(CFStringRef *)dlsym(MR, "kMRMediaRemoteNowPlayingInfoTitle");
  KeyArtist = *(CFStringRef *)dlsym(MR, "kMRMediaRemoteNowPlayingInfoArtist");
  KeyAlbum = *(CFStringRef *)dlsym(MR, "kMRMediaRemoteNowPlayingInfoAlbum");
  KeyDuration = *(CFStringRef *)dlsym(MR, "kMRMediaRemoteNowPlayingInfoDuration");
  KeyElapsed = *(CFStringRef *)dlsym(MR, "kMRMediaRemoteNowPlayingInfoElapsedTime");
  KeyRate = *(CFStringRef *)dlsym(MR, "kMRMediaRemoteNowPlayingInfoPlaybackRate");
  KeyArtwork = *(CFStringRef *)dlsym(MR, "kMRMediaRemoteNowPlayingInfoArtworkData");
  KeyItemID = *(CFStringRef *)dlsym(MR, "kMRMediaRemoteNowPlayingInfoContentItemIdentifier");
  if (!MRGetInfo || !MRGetIsPlaying || !MRSendCommand) {
    fprintf(stderr, "shelf-np: MediaRemote lacks the needed functions\n");
    exit(2);
  }
}

// The reader prints nothing to stdout on its own, so a closed stdout is
// safe to ignore.
static void ignoreSigpipe(void) { signal(SIGPIPE, SIG_IGN); }

// shelf_np_run is the bundle entry the Apple-signed host calls. It runs
// until stdin is closed or "quit" arrives.
void shelf_np_run(void) {
  loadMediaRemote();
  ignoreSigpipe();
  workQ = dispatch_queue_create("shelf.np.work", NULL);
  workSem = dispatch_semaphore_create(0);
  if (MRRegister) MRRegister(dispatch_get_main_queue());
  observeNotifications();

  // First sample: the reader starts with a real state.
  poll();

  // Timer thread: read when the system said something changed, and at
  // least once a second (that anchor is what the reader extrapolates a
  // moving progress bar from). The timer itself is cheap; the 1s gate
  // keeps an idle system down to one MediaRemote round trip per second.
  dispatch_source_t timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0,
                                                   dispatch_get_global_queue(QOS_CLASS_UTILITY, 0));
  dispatch_source_set_timer(timer, dispatch_time(DISPATCH_TIME_NOW, 200 * NSEC_PER_MSEC),
                            200 * NSEC_PER_MSEC, 50 * NSEC_PER_MSEC);
  dispatch_source_set_event_handler(timer, ^{
    double now = nowUnix();
    if (gotNotify || now - lastSample >= 1.0) {
      gotNotify = 0;
      poll();
    }
  });
  dispatch_resume(timer);

  char line[256];
  while (fgets(line, sizeof line, stdin)) {
    size_t n = strlen(line);
    while (n && (line[n - 1] == '\n' || line[n - 1] == '\r')) line[--n] = 0;
    if (!n) continue;
    int quit = 0;
    handleCommand(line, &quit);
    if (quit) break;
  }
  dispatch_source_cancel(timer);
}
