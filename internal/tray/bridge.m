#import <Cocoa/Cocoa.h>
#include "bridge.h"

@interface GWPanel : NSPanel
@end

@implementation GWPanel
- (BOOL)canBecomeKeyWindow { return NO; }
@end

@interface GWDetailPanel : NSPanel
@end

@implementation GWDetailPanel
- (BOOL)canBecomeKeyWindow { return YES; }
@end

@interface GWTray : NSObject <NSWindowDelegate>
@property (strong) GWPanel *panel;
@property (strong) GWDetailPanel *detailPanel;
@property (strong) NSView *dot;
@property (strong) NSTextField *label;
@property (strong) NSButton *button;
@property (strong) NSButton *chevron;
@property (assign) int state;
@property (assign) BOOL detailVisible;

@property (strong) NSView *listContainer;
@property (strong) NSView *detailContainer;

@property (strong) NSStackView *eventsStack;
@property (strong) NSTextField *upcomingHeader;
@property (strong) NSView *separator;
@property (strong) NSStackView *transcriptsStack;
@property (strong) NSScrollView *transcriptsScroll;

@property (strong) NSTextField *detailTitle;
@property (strong) NSTextField *detailMeta;
@property (strong) NSTextView *detailText;
@end

static GWTray *tray = nil;

@implementation GWTray

- (void)setup {
    NSRect frame = NSMakeRect(0, 0, 340, 48);

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
    self.panel.delegate = self;

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
    self.label.frame = NSMakeRect(34, 14, 142, 20);
    self.label.textColor = [NSColor whiteColor];
    self.label.font = [NSFont systemFontOfSize:13 weight:NSFontWeightMedium];
    self.label.lineBreakMode = NSLineBreakByTruncatingTail;
    [bg addSubview:self.label];

    self.button = [NSButton buttonWithTitle:@"Record" target:self action:@selector(clicked:)];
    self.button.frame = NSMakeRect(184, 10, 76, 28);
    self.button.bezelStyle = NSBezelStyleRounded;
    self.button.enabled = NO;
    [bg addSubview:self.button];

    self.chevron = [NSButton buttonWithImage:[NSImage imageWithSystemSymbolName:@"chevron.down" accessibilityDescription:@"Toggle panel"]
        target:self action:@selector(togglePanel:)];
    self.chevron.frame = NSMakeRect(268, 10, 30, 28);
    self.chevron.bezelStyle = NSBezelStyleRounded;
    self.chevron.bordered = NO;
    self.chevron.contentTintColor = [NSColor secondaryLabelColor];
    [bg addSubview:self.chevron];

    NSButton *closeBtn = [NSButton buttonWithImage:[NSImage imageWithSystemSymbolName:@"xmark" accessibilityDescription:@"Quit"]
        target:self action:@selector(quit:)];
    closeBtn.frame = NSMakeRect(298, 10, 30, 28);
    closeBtn.bezelStyle = NSBezelStyleRounded;
    closeBtn.bordered = NO;
    closeBtn.contentTintColor = [NSColor secondaryLabelColor];
    [bg addSubview:closeBtn];

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
    CGFloat x = (screen.frame.size.width - 340) / 2;
    [self.panel setFrameOrigin:NSMakePoint(x, 100)];
    [self.panel orderFrontRegardless];

    [self setupDetailPanel];
}

- (void)setupDetailPanel {
    NSRect frame = NSMakeRect(0, 0, 320, 480);

    self.detailPanel = [[GWDetailPanel alloc] initWithContentRect:frame
        styleMask:NSWindowStyleMaskBorderless
        backing:NSBackingStoreBuffered defer:NO];
    self.detailPanel.level = NSFloatingWindowLevel;
    self.detailPanel.backgroundColor = [NSColor clearColor];
    self.detailPanel.opaque = NO;
    self.detailPanel.hasShadow = YES;
    self.detailPanel.hidesOnDeactivate = NO;
    self.detailPanel.collectionBehavior =
        NSWindowCollectionBehaviorCanJoinAllSpaces |
        NSWindowCollectionBehaviorStationary;

    NSVisualEffectView *bg = [[NSVisualEffectView alloc] initWithFrame:frame];
    bg.material = NSVisualEffectMaterialHUDWindow;
    bg.state = NSVisualEffectStateActive;
    bg.wantsLayer = YES;
    bg.layer.cornerRadius = 12;
    bg.layer.masksToBounds = YES;
    bg.appearance = [NSAppearance appearanceNamed:NSAppearanceNameVibrantDark];
    self.detailPanel.contentView = bg;

    [self setupListView:bg];
    [self setupDetailView:bg];

    self.detailContainer.hidden = YES;
    self.detailVisible = NO;
}

- (void)setupListView:(NSView *)parent {
    self.listContainer = [[NSView alloc] initWithFrame:parent.bounds];
    self.listContainer.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    [parent addSubview:self.listContainer];

    CGFloat y = parent.bounds.size.height;

    self.upcomingHeader = [NSTextField labelWithString:@"UPCOMING"];
    self.upcomingHeader.frame = NSMakeRect(16, y - 32, 288, 16);
    self.upcomingHeader.textColor = [NSColor secondaryLabelColor];
    self.upcomingHeader.font = [NSFont systemFontOfSize:11 weight:NSFontWeightSemibold];
    [self.listContainer addSubview:self.upcomingHeader];

    self.eventsStack = [NSStackView stackViewWithViews:@[]];
    self.eventsStack.orientation = NSUserInterfaceLayoutOrientationVertical;
    self.eventsStack.alignment = NSLayoutAttributeLeading;
    self.eventsStack.spacing = 2;
    self.eventsStack.frame = NSMakeRect(16, y - 32 - 120, 288, 120);
    [self.listContainer addSubview:self.eventsStack];

    self.separator = [[NSView alloc] initWithFrame:NSMakeRect(16, y - 32 - 120 - 17, 288, 1)];
    self.separator.wantsLayer = YES;
    self.separator.layer.backgroundColor = [NSColor separatorColor].CGColor;
    [self.listContainer addSubview:self.separator];

    NSTextField *recentHeader = [NSTextField labelWithString:@"RECENT"];
    recentHeader.frame = NSMakeRect(16, y - 32 - 120 - 17 - 24, 288, 16);
    recentHeader.textColor = [NSColor secondaryLabelColor];
    recentHeader.font = [NSFont systemFontOfSize:11 weight:NSFontWeightSemibold];
    [self.listContainer addSubview:recentHeader];

    self.transcriptsStack = [NSStackView stackViewWithViews:@[]];
    self.transcriptsStack.orientation = NSUserInterfaceLayoutOrientationVertical;
    self.transcriptsStack.alignment = NSLayoutAttributeLeading;
    self.transcriptsStack.spacing = 0;
    self.transcriptsStack.translatesAutoresizingMaskIntoConstraints = NO;

    self.transcriptsScroll = [[NSScrollView alloc] initWithFrame:
        NSMakeRect(8, 8, 304, y - 32 - 120 - 17 - 24 - 16)];
    self.transcriptsScroll.hasVerticalScroller = YES;
    self.transcriptsScroll.drawsBackground = NO;
    self.transcriptsScroll.documentView = self.transcriptsStack;

    [NSLayoutConstraint activateConstraints:@[
        [self.transcriptsStack.leadingAnchor constraintEqualToAnchor:self.transcriptsScroll.contentView.leadingAnchor],
        [self.transcriptsStack.trailingAnchor constraintEqualToAnchor:self.transcriptsScroll.contentView.trailingAnchor],
        [self.transcriptsStack.topAnchor constraintEqualToAnchor:self.transcriptsScroll.contentView.topAnchor],
    ]];

    [self.listContainer addSubview:self.transcriptsScroll];
}

- (void)setupDetailView:(NSView *)parent {
    self.detailContainer = [[NSView alloc] initWithFrame:parent.bounds];
    self.detailContainer.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    [parent addSubview:self.detailContainer];

    CGFloat w = parent.bounds.size.width;
    CGFloat h = parent.bounds.size.height;

    NSButton *backBtn = [NSButton buttonWithTitle:@"\u2190" target:self action:@selector(backToList:)];
    backBtn.frame = NSMakeRect(8, h - 36, 30, 28);
    backBtn.bezelStyle = NSBezelStyleRounded;
    backBtn.bordered = NO;
    backBtn.font = [NSFont systemFontOfSize:16];
    backBtn.contentTintColor = [NSColor controlAccentColor];
    [self.detailContainer addSubview:backBtn];

    self.detailTitle = [NSTextField labelWithString:@""];
    self.detailTitle.frame = NSMakeRect(38, h - 34, w - 54, 20);
    self.detailTitle.textColor = [NSColor whiteColor];
    self.detailTitle.font = [NSFont systemFontOfSize:14 weight:NSFontWeightSemibold];
    self.detailTitle.lineBreakMode = NSLineBreakByTruncatingTail;
    [self.detailContainer addSubview:self.detailTitle];

    self.detailMeta = [NSTextField labelWithString:@""];
    self.detailMeta.frame = NSMakeRect(38, h - 54, w - 54, 16);
    self.detailMeta.textColor = [NSColor secondaryLabelColor];
    self.detailMeta.font = [NSFont systemFontOfSize:11];
    [self.detailContainer addSubview:self.detailMeta];

    NSScrollView *textScroll = [[NSScrollView alloc] initWithFrame:NSMakeRect(12, 8, w - 24, h - 68)];
    textScroll.hasVerticalScroller = YES;
    textScroll.drawsBackground = NO;

    self.detailText = [[NSTextView alloc] initWithFrame:NSMakeRect(0, 0, w - 24, h - 68)];
    self.detailText.editable = NO;
    self.detailText.selectable = YES;
    self.detailText.drawsBackground = NO;
    self.detailText.textColor = [NSColor labelColor];
    self.detailText.font = [NSFont systemFontOfSize:12];
    self.detailText.textContainerInset = NSMakeSize(4, 4);
    self.detailText.autoresizingMask = NSViewWidthSizable;
    [self.detailText setMinSize:NSMakeSize(0, h - 68)];
    [self.detailText setMaxSize:NSMakeSize(FLT_MAX, FLT_MAX)];
    self.detailText.textContainer.containerSize = NSMakeSize(w - 32, FLT_MAX);
    self.detailText.textContainer.widthTracksTextView = YES;
    self.detailText.verticallyResizable = YES;

    textScroll.documentView = self.detailText;
    [self.detailContainer addSubview:textScroll];
}

- (void)positionDetailPanel {
    NSRect pillFrame = self.panel.frame;
    CGFloat detailWidth = 320;
    CGFloat x = pillFrame.origin.x + (pillFrame.size.width - detailWidth) / 2;
    CGFloat y = pillFrame.origin.y - 4 - 480;
    [self.detailPanel setFrameOrigin:NSMakePoint(x, y)];
}

- (void)windowDidMove:(NSNotification *)notification {
    if (notification.object == self.panel && self.detailVisible) {
        [self positionDetailPanel];
    }
}

- (void)togglePanel:(id)sender {
    goTrayOnTogglePanel();
}

- (void)showDetailPanel {
    if (self.detailVisible) return;
    self.detailVisible = YES;
    self.listContainer.hidden = NO;
    self.detailContainer.hidden = YES;
    [self positionDetailPanel];
    [self.detailPanel orderFrontRegardless];
    self.chevron.image = [NSImage imageWithSystemSymbolName:@"chevron.up" accessibilityDescription:@"Close panel"];
}

- (void)hideDetailPanel {
    if (!self.detailVisible) return;
    self.detailVisible = NO;
    [self.detailPanel orderOut:nil];
    self.chevron.image = [NSImage imageWithSystemSymbolName:@"chevron.down" accessibilityDescription:@"Toggle panel"];
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

- (void)backToList:(id)sender {
    self.listContainer.hidden = NO;
    self.detailContainer.hidden = YES;
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

- (void)updateEvents:(NSString *)jsonStr {
    NSData *data = [jsonStr dataUsingEncoding:NSUTF8StringEncoding];
    NSArray *events = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    if (!events) return;

    for (NSView *v in [self.eventsStack.arrangedSubviews copy]) {
        [self.eventsStack removeArrangedSubview:v];
        [v removeFromSuperview];
    }

    BOOL hasEvents = events.count > 0;
    self.upcomingHeader.hidden = !hasEvents;
    self.eventsStack.hidden = !hasEvents;
    self.separator.hidden = !hasEvents;

    if (!hasEvents) {
        [self relayoutListForEvents:NO];
        return;
    }

    NSDateFormatter *timeFmt = [[NSDateFormatter alloc] init];
    timeFmt.dateFormat = @"h:mm a";

    NSISO8601DateFormatter *isoFmt = [[NSISO8601DateFormatter alloc] init];

    for (NSDictionary *evt in events) {
        NSView *row = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, 288, 22)];
        row.translatesAutoresizingMaskIntoConstraints = NO;
        [row.heightAnchor constraintEqualToConstant:22].active = YES;
        [row.widthAnchor constraintEqualToConstant:288].active = YES;

        NSDate *startDate = [isoFmt dateFromString:evt[@"start"]];
        NSString *timeStr = startDate ? [timeFmt stringFromDate:startDate] : @"";

        NSTextField *timeLbl = [NSTextField labelWithString:timeStr];
        timeLbl.frame = NSMakeRect(0, 2, 70, 18);
        timeLbl.textColor = [NSColor secondaryLabelColor];
        timeLbl.font = [NSFont monospacedDigitSystemFontOfSize:11 weight:NSFontWeightRegular];
        [row addSubview:timeLbl];

        NSTextField *titleLbl = [NSTextField labelWithString:evt[@"title"]];
        titleLbl.frame = NSMakeRect(74, 2, 214, 18);
        titleLbl.textColor = [NSColor whiteColor];
        titleLbl.font = [NSFont systemFontOfSize:12 weight:NSFontWeightMedium];
        titleLbl.lineBreakMode = NSLineBreakByTruncatingTail;
        [row addSubview:titleLbl];

        [self.eventsStack addArrangedSubview:row];
    }

    [self relayoutListForEvents:YES];
}

- (void)relayoutListForEvents:(BOOL)hasEvents {
    CGFloat h = self.detailPanel.frame.size.height;

    if (hasEvents) {
        CGFloat eventsHeight = self.eventsStack.arrangedSubviews.count * 24;
        if (eventsHeight > 120) eventsHeight = 120;
        self.eventsStack.frame = NSMakeRect(16, h - 32 - eventsHeight, 288, eventsHeight);
        self.separator.frame = NSMakeRect(16, h - 32 - eventsHeight - 17, 288, 1);

        NSView *recentHeader = nil;
        for (NSView *v in self.listContainer.subviews) {
            if ([v isKindOfClass:[NSTextField class]] && [((NSTextField *)v).stringValue isEqualToString:@"RECENT"]) {
                recentHeader = v;
                break;
            }
        }
        if (recentHeader) {
            recentHeader.frame = NSMakeRect(16, h - 32 - eventsHeight - 17 - 24, 288, 16);
        }
        self.transcriptsScroll.frame = NSMakeRect(8, 8, 304, h - 32 - eventsHeight - 17 - 24 - 16);
    } else {
        NSView *recentHeader = nil;
        for (NSView *v in self.listContainer.subviews) {
            if ([v isKindOfClass:[NSTextField class]] && [((NSTextField *)v).stringValue isEqualToString:@"RECENT"]) {
                recentHeader = v;
                break;
            }
        }
        if (recentHeader) {
            recentHeader.frame = NSMakeRect(16, h - 32, 288, 16);
        }
        self.transcriptsScroll.frame = NSMakeRect(8, 8, 304, h - 32 - 24);
    }
}

- (void)updateTranscripts:(NSString *)jsonStr {
    NSData *data = [jsonStr dataUsingEncoding:NSUTF8StringEncoding];
    NSArray *transcripts = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    if (!transcripts) return;

    for (NSView *v in [self.transcriptsStack.arrangedSubviews copy]) {
        [self.transcriptsStack removeArrangedSubview:v];
        [v removeFromSuperview];
    }

    NSDateFormatter *dateFmt = [[NSDateFormatter alloc] init];
    dateFmt.dateFormat = @"MMM d";

    NSISO8601DateFormatter *isoFmt = [[NSISO8601DateFormatter alloc] init];

    for (NSDictionary *t in transcripts) {
        NSView *row = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, 296, 44)];
        row.wantsLayer = YES;
        row.translatesAutoresizingMaskIntoConstraints = NO;
        [row.heightAnchor constraintEqualToConstant:44].active = YES;
        [row.widthAnchor constraintEqualToConstant:296].active = YES;

        NSString *title = t[@"title"];
        if (!title || [title length] == 0) title = @"Untitled";
        NSTextField *titleLbl = [NSTextField labelWithString:title];
        titleLbl.frame = NSMakeRect(8, 22, 280, 18);
        titleLbl.textColor = [NSColor whiteColor];
        titleLbl.font = [NSFont systemFontOfSize:13 weight:NSFontWeightMedium];
        titleLbl.lineBreakMode = NSLineBreakByTruncatingTail;
        [row addSubview:titleLbl];

        NSDate *date = [isoFmt dateFromString:t[@"date"]];
        NSString *dateStr = date ? [dateFmt stringFromDate:date] : @"";
        int duration = [t[@"duration_seconds"] intValue];
        NSString *durStr = [NSString stringWithFormat:@"%dm", duration / 60];
        NSString *subtitle = [NSString stringWithFormat:@"%@ \u00B7 %@", dateStr, durStr];

        NSTextField *subLbl = [NSTextField labelWithString:subtitle];
        subLbl.frame = NSMakeRect(8, 4, 280, 16);
        subLbl.textColor = [NSColor secondaryLabelColor];
        subLbl.font = [NSFont systemFontOfSize:11];
        [row addSubview:subLbl];

        NSString *tid = t[@"id"];
        NSClickGestureRecognizer *click = [[NSClickGestureRecognizer alloc]
            initWithTarget:self action:@selector(transcriptRowClicked:)];
        row.toolTip = tid;
        [row addGestureRecognizer:click];

        [self.transcriptsStack addArrangedSubview:row];
    }
}

- (void)transcriptRowClicked:(NSClickGestureRecognizer *)sender {
    NSString *tid = sender.view.toolTip;
    if (tid) {
        goTrayOnSelectTranscript((char *)tid.UTF8String);
    }
}

- (void)showTranscriptDetail:(NSString *)jsonStr {
    NSData *data = [jsonStr dataUsingEncoding:NSUTF8StringEncoding];
    NSDictionary *t = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    if (!t) return;

    self.detailTitle.stringValue = t[@"title"] ?: @"Untitled";

    NSISO8601DateFormatter *isoFmt = [[NSISO8601DateFormatter alloc] init];
    NSDate *date = [isoFmt dateFromString:t[@"date"]];
    NSDateFormatter *dateFmt = [[NSDateFormatter alloc] init];
    dateFmt.dateFormat = @"MMM d, yyyy";
    NSString *dateStr = date ? [dateFmt stringFromDate:date] : @"";
    int duration = [t[@"duration_seconds"] intValue];
    NSString *durStr = [NSString stringWithFormat:@"%dm", duration / 60];
    NSString *source = t[@"source"] ?: @"";
    self.detailMeta.stringValue = [NSString stringWithFormat:@"%@ \u00B7 %@ \u00B7 %@", dateStr, durStr, source];

    NSString *fullText = t[@"full_text"] ?: @"";
    [self.detailText setString:fullText];

    self.listContainer.hidden = YES;
    self.detailContainer.hidden = NO;
}

@end

void TrayBridgeSetup(void) {
    tray = [[GWTray alloc] init];
    [tray setup];
    goTrayOnReady();
}

void TrayBridgeRun(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

        NSMenu *mainMenu = [[NSMenu alloc] init];
        NSMenuItem *appMenuItem = [[NSMenuItem alloc] init];
        [mainMenu addItem:appMenuItem];
        NSMenu *appMenu = [[NSMenu alloc] init];
        [appMenu addItemWithTitle:@"Quit Ghostwriter" action:@selector(terminate:) keyEquivalent:@"q"];
        appMenuItem.submenu = appMenu;
        [NSApp setMainMenu:mainMenu];

        TrayBridgeSetup();
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

void TrayBridgeTogglePanel(void) {
    if (!tray) return;
    dispatch_async(dispatch_get_main_queue(), ^{
        if (tray.detailVisible) {
            [tray hideDetailPanel];
        } else {
            [tray showDetailPanel];
        }
    });
}

void TrayBridgeUpdateTranscripts(const char *jsonData) {
    if (!tray) return;
    NSString *str = [NSString stringWithUTF8String:jsonData];
    dispatch_async(dispatch_get_main_queue(), ^{
        [tray updateTranscripts:str];
    });
}

void TrayBridgeUpdateEvents(const char *jsonData) {
    if (!tray) return;
    NSString *str = [NSString stringWithUTF8String:jsonData];
    dispatch_async(dispatch_get_main_queue(), ^{
        [tray updateEvents:str];
    });
}

void TrayBridgeShowTranscriptDetail(const char *jsonData) {
    if (!tray) return;
    NSString *str = [NSString stringWithUTF8String:jsonData];
    dispatch_async(dispatch_get_main_queue(), ^{
        [tray showTranscriptDetail:str];
    });
}

void TrayBridgeQuit(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp terminate:nil];
    });
}
