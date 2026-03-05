#import <Foundation/Foundation.h>
#import <AppKit/NSApplication.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <CoreMedia/CoreMedia.h>
#import <AVFoundation/AVFoundation.h>
#include "sckit_bridge.h"

static dispatch_once_t appInitToken;

void SCKitBridgeEnsureAppInit(void) {
    dispatch_once(&appInitToken, ^{
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    });
}

void SCKitBridgeRunMainLoop(void) {
    [NSApp run];
}

void SCKitBridgeQuitMainLoop(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp terminate:nil];
    });
}

API_AVAILABLE(macos(12.3))
@interface SCKitAudioDelegate : NSObject <SCStreamOutput, SCStreamDelegate>
@property (nonatomic, strong) NSMutableData *audioData;
@property (nonatomic, assign) int sampleRate;
@property (nonatomic, assign) int channels;
@end

API_AVAILABLE(macos(12.3))
@implementation SCKitAudioDelegate

- (instancetype)init {
    self = [super init];
    if (self) {
        _audioData = [NSMutableData data];
        _sampleRate = 48000;
        _channels = 2;
    }
    return self;
}

- (void)stream:(SCStream *)stream
    didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer
               ofType:(SCStreamOutputType)type {
    if (type != SCStreamOutputTypeAudio) return;
    if (!CMSampleBufferDataIsReady(sampleBuffer)) return;

    CMFormatDescriptionRef fmt = CMSampleBufferGetFormatDescription(sampleBuffer);
    const AudioStreamBasicDescription *asbd = CMAudioFormatDescriptionGetStreamBasicDescription(fmt);
    if (asbd) {
        self.sampleRate = (int)asbd->mSampleRate;
        self.channels = (int)asbd->mChannelsPerFrame;
    }

    CMBlockBufferRef blockBuffer = CMSampleBufferGetDataBuffer(sampleBuffer);
    if (!blockBuffer) return;

    size_t length = 0;
    char *dataPointer = NULL;
    OSStatus status = CMBlockBufferGetDataPointer(blockBuffer, 0, NULL, &length, &dataPointer);
    if (status != kCMBlockBufferNoErr || !dataPointer) return;

    @synchronized (self.audioData) {
        [self.audioData appendBytes:dataPointer length:length];
    }
}

- (void)stream:(SCStream *)stream didStopWithError:(NSError *)error {
    if (error) {
        NSLog(@"SCKit stream error: %@", error);
    }
}

@end

static SCStream *activeStream API_AVAILABLE(macos(12.3)) = nil;
static SCKitAudioDelegate *activeDelegate API_AVAILABLE(macos(12.3)) = nil;

bool SCKitBridgeIsAvailable(void) {
    if (@available(macOS 12.3, *)) {
        return NSClassFromString(@"SCShareableContent") != nil;
    }
    return false;
}

bool SCKitBridgeHasPermission(void) {
    if (@available(macOS 12.3, *)) {
        __block bool hasPermission = false;
        dispatch_semaphore_t sem = dispatch_semaphore_create(0);

        [SCShareableContent getShareableContentWithCompletionHandler:^(SCShareableContent *content, NSError *error) {
            hasPermission = (error == nil && content != nil);
            dispatch_semaphore_signal(sem);
        }];

        dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));
        return hasPermission;
    }
    return false;
}

int SCKitBridgeStartCapture(const char *appName) {
    if (@available(macOS 12.3, *)) {
        __block int result = -1;
        dispatch_semaphore_t sem = dispatch_semaphore_create(0);

        [SCShareableContent getShareableContentWithCompletionHandler:^(SCShareableContent *content, NSError *error) {
            if (error || !content) {
                NSLog(@"SCKit: failed to get shareable content: %@", error);
                dispatch_semaphore_signal(sem);
                return;
            }

            SCContentFilter *filter = nil;
            NSString *targetApp = appName ? [NSString stringWithUTF8String:appName] : nil;

            if (targetApp.length > 0) {
                SCRunningApplication *matched = nil;
                for (SCRunningApplication *app in content.applications) {
                    if ([app.applicationName localizedCaseInsensitiveContainsString:targetApp]) {
                        matched = app;
                        break;
                    }
                }
                if (matched) {
                    filter = [[SCContentFilter alloc] initWithDisplay:content.displays.firstObject
                                                   includingApplications:@[matched]
                                                   exceptingWindows:@[]];
                }
            }

            if (!filter) {
                filter = [[SCContentFilter alloc] initWithDisplay:content.displays.firstObject
                                               excludingWindows:@[]];
            }
            SCStreamConfiguration *config = [[SCStreamConfiguration alloc] init];
            config.capturesAudio = YES;
            config.excludesCurrentProcessAudio = YES;
            config.width = 2;
            config.height = 2;
            config.minimumFrameInterval = CMTimeMake(1, 1);

            if (@available(macOS 13.0, *)) {
                config.channelCount = 2;
                config.sampleRate = 48000;
            }

            activeDelegate = [[SCKitAudioDelegate alloc] init];
            activeStream = [[SCStream alloc] initWithFilter:filter configuration:config delegate:activeDelegate];

            NSError *addOutputError = nil;
            [activeStream addStreamOutput:activeDelegate
                                     type:SCStreamOutputTypeAudio
                       sampleHandlerQueue:dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0)
                                    error:&addOutputError];

            if (addOutputError) {
                NSLog(@"SCKit: failed to add stream output: %@", addOutputError);
                activeStream = nil;
                activeDelegate = nil;
                dispatch_semaphore_signal(sem);
                return;
            }

            [activeStream startCaptureWithCompletionHandler:^(NSError *startError) {
                if (startError) {
                    NSLog(@"SCKit: failed to start capture: %@", startError);
                    activeStream = nil;
                    activeDelegate = nil;
                } else {
                    result = 0;
                }
                dispatch_semaphore_signal(sem);
            }];
        }];

        dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC));
        return result;
    }
    return -1;
}

SCKitAudioBuffer SCKitBridgeStopCapture(void) {
    SCKitAudioBuffer buf = {NULL, 0, 0, 0};

    if (@available(macOS 12.3, *)) {
        if (!activeStream || !activeDelegate) return buf;

        dispatch_semaphore_t sem = dispatch_semaphore_create(0);

        [activeStream stopCaptureWithCompletionHandler:^(NSError *error) {
            if (error) {
                NSLog(@"SCKit: stop error: %@", error);
            }
            dispatch_semaphore_signal(sem);
        }];

        dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));

        @synchronized (activeDelegate.audioData) {
            NSUInteger byteLen = activeDelegate.audioData.length;
            if (byteLen > 0) {
                buf.samples = (float *)malloc(byteLen);
                if (buf.samples) {
                    memcpy(buf.samples, activeDelegate.audioData.bytes, byteLen);
                    buf.sampleCount = byteLen / sizeof(float);
                    buf.sampleRate = activeDelegate.sampleRate;
                    buf.channels = activeDelegate.channels;
                }
            }
        }

        activeStream = nil;
        activeDelegate = nil;
    }

    return buf;
}

void SCKitBridgeFreeBuffer(SCKitAudioBuffer buf) {
    if (buf.samples) {
        free(buf.samples);
    }
}
