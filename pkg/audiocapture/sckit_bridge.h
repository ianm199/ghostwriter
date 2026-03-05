#ifndef SCKIT_BRIDGE_H
#define SCKIT_BRIDGE_H

#include <stdbool.h>
#include <stdint.h>

typedef struct {
    float *samples;
    int64_t sampleCount;
    int sampleRate;
    int channels;
} SCKitAudioBuffer;

void SCKitBridgeEnsureAppInit(void);
void SCKitBridgeRunMainLoop(void);
void SCKitBridgeQuitMainLoop(void);
bool SCKitBridgeIsAvailable(void);
bool SCKitBridgeHasPermission(void);
int  SCKitBridgeStartCapture(const char *appName);
SCKitAudioBuffer SCKitBridgeStopCapture(void);
void SCKitBridgeFreeBuffer(SCKitAudioBuffer buf);

#endif
