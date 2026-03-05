#import <Cocoa/Cocoa.h>
#include "bridge.h"

@interface GWPanel : NSPanel
@end

@implementation GWPanel
- (BOOL)canBecomeKeyWindow { return NO; }
@end

@interface GWTray : NSObject
@property (strong) GWPanel *panel;
@property (strong) NSView *dot;
@property (strong) NSTextField *label;
@property (strong) NSButton *button;
@property (assign) int state;
@end

static GWTray *tray = nil;

@implementation GWTray

- (void)setup {
    NSRect frame = NSMakeRect(0, 0, 280, 48);

    self.panel = [[GWPanel alloc] initWithContentRect:frame
        styleMask:NSWindowStyleMaskBorderless
        backing:NSBackingStoreBuffered defer:NO];
    self.panel.level = NSFloatingWindowLevel;
    self.panel.backgroundColor = [NSColor clearColor];
    self.panel.opaque = NO;
    self.panel.hasShadow = YES;
    self.panel.movableByWindowBackground = YES;
    self.panel.hidesOnDeactivate = NO;
    self.panel.collectionBehavior =
        NSWindowCollectionBehaviorCanJoinAllSpaces |
        NSWindowCollectionBehaviorStationary;

    NSVisualEffectView *bg = [[NSVisualEffectView alloc] initWithFrame:frame];
    bg.material = NSVisualEffectMaterialHUDWindow;
    bg.state = NSVisualEffectStateActive;
    bg.wantsLayer = YES;
    bg.layer.cornerRadius = 12;
    bg.layer.masksToBounds = YES;
    bg.appearance = [NSAppearance appearanceNamed:NSAppearanceNameVibrantDark];
    self.panel.contentView = bg;

    self.dot = [[NSView alloc] initWithFrame:NSMakeRect(16, 19, 10, 10)];
    self.dot.wantsLayer = YES;
    self.dot.layer.cornerRadius = 5;
    self.dot.layer.backgroundColor = [NSColor grayColor].CGColor;
    [bg addSubview:self.dot];

    self.label = [NSTextField labelWithString:@"Connecting..."];
    self.label.frame = NSMakeRect(34, 14, 152, 20);
    self.label.textColor = [NSColor whiteColor];
    self.label.font = [NSFont systemFontOfSize:13 weight:NSFontWeightMedium];
    self.label.lineBreakMode = NSLineBreakByTruncatingTail;
    [bg addSubview:self.label];

    self.button = [NSButton buttonWithTitle:@"Record" target:self action:@selector(clicked:)];
    self.button.frame = NSMakeRect(194, 10, 76, 28);
    self.button.bezelStyle = NSBezelStyleRounded;
    self.button.enabled = NO;
    [bg addSubview:self.button];

    NSMenu *menu = [[NSMenu alloc] init];
    NSMenuItem *transcripts = [menu addItemWithTitle:@"Open Transcripts"
        action:@selector(openTranscripts:) keyEquivalent:@""];
    transcripts.target = self;
    [menu addItem:[NSMenuItem separatorItem]];
    NSMenuItem *quit = [menu addItemWithTitle:@"Quit"
        action:@selector(quit:) keyEquivalent:@"q"];
    quit.target = self;
    bg.menu = menu;

    NSScreen *screen = [NSScreen mainScreen];
    CGFloat x = (screen.frame.size.width - 280) / 2;
    [self.panel setFrameOrigin:NSMakePoint(x, 100)];
    [self.panel orderFrontRegardless];
}

- (void)clicked:(id)sender {
    if (self.state == 1) goTrayOnStart();
    else if (self.state == 2) goTrayOnStop();
}

- (void)openTranscripts:(id)sender {
    goTrayOnOpenTranscripts();
}

- (void)quit:(id)sender {
    goTrayOnQuit();
}

- (void)updateStatus:(NSString *)text state:(int)s {
    self.state = s;
    self.label.stringValue = text;

    NSColor *dotColor;
    NSString *btnTitle;
    BOOL btnEnabled;

    switch (s) {
        case 0:
            dotColor = [NSColor grayColor];
            btnTitle = @"Record";
            btnEnabled = NO;
            break;
        case 1:
            dotColor = [NSColor systemGreenColor];
            btnTitle = @"Record";
            btnEnabled = YES;
            break;
        case 2:
            dotColor = [NSColor systemRedColor];
            btnTitle = @"Stop";
            btnEnabled = YES;
            break;
        case 3:
            dotColor = [NSColor systemOrangeColor];
            btnTitle = @"Wait...";
            btnEnabled = NO;
            break;
        default:
            return;
    }

    self.dot.layer.backgroundColor = dotColor.CGColor;
    self.button.title = btnTitle;
    self.button.enabled = btnEnabled;
}

@end

void TrayBridgeRun(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

        tray = [[GWTray alloc] init];
        [tray setup];

        goTrayOnReady();
        [NSApp run];
    }
}

void TrayBridgeUpdateStatus(const char *text, int state) {
    if (!tray) return;
    NSString *str = [NSString stringWithUTF8String:text];
    dispatch_async(dispatch_get_main_queue(), ^{
        [tray updateStatus:str state:state];
    });
}

void TrayBridgeQuit(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp terminate:nil];
    });
}
