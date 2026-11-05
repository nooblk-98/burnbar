#import <AppKit/AppKit.h>
#include <stdlib.h>
#include "macos.h"
extern void bb_tick(void);
extern void bb_action(int);
extern char *bb_state(void);
extern char *bb_save(char *);

@interface BurnBarDelegate : NSObject <NSApplicationDelegate, NSPopoverDelegate>
@property NSStatusItem *status;
@property NSPopover *popover;
@property NSTextView *summary;
@property NSStackView *meters;
@property NSWindow *settings;
@property NSMutableDictionary<NSString *, NSControl *> *fields;
@property NSDictionary *state;
@property NSMenu *menu;
@property NSDate *deadline;
@property NSString *automaticNotice;
@property BOOL smoke;
@end

static NSTextField *label(NSString *text) { return [NSTextField labelWithString:text ?: @""]; }
static NSButton *button(NSString *title,id target,SEL action) {
 NSButton *b=[NSButton buttonWithTitle:title target:target action:action];return b;
}
static NSDictionary *readState(void) {
 char *raw=bb_state();if(!raw)return @{};
 NSData *data=[NSData dataWithBytes:raw length:strlen(raw)];free(raw);
 return [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] ?: @{};
}
@implementation BurnBarDelegate
- (void)applicationDidFinishLaunching:(NSNotification *)notification {
 if(self.smoke){fprintf(stderr,"AppKit finished launching\n");fflush(stderr);}
 if(self.smoke){fprintf(stderr,"Creating status item\n");fflush(stderr);}
 self.status=[[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
 self.status.button.title=@"BurnBar";self.status.button.toolTip=@"Claude and Codex usage";
 self.status.button.target=self;self.status.button.action=@selector(clicked:);
 [self.status.button sendActionOn:NSEventMaskLeftMouseUp|NSEventMaskRightMouseUp];
 self.menu=[[NSMenu alloc] initWithTitle:@"BurnBar"];
 NSArray *titles=@[@"Authorize Claude Keychain…",@"Refresh",@"Preferences…",@"Snooze for one hour",@"Resume notices",@"Check for updates",@"Release notes",@"Quit BurnBar"];
 SEL actions[]={@selector(authorize:),@selector(refresh:),@selector(preferences:),@selector(snooze:),@selector(resume:),@selector(updates:),@selector(releases:),@selector(quit:)};
 for(NSUInteger i=0;i<titles.count;i++){NSMenuItem *item=[[NSMenuItem alloc]initWithTitle:titles[i] action:actions[i] keyEquivalent:i==7?@"q":@""];item.target=self;[self.menu addItem:item];}
 self.popover=[[NSPopover alloc]init];self.popover.behavior=NSPopoverBehaviorTransient;self.popover.delegate=self;
 NSViewController *vc=[[NSViewController alloc]init];vc.view=[[NSView alloc]initWithFrame:NSMakeRect(0,0,370,560)];
 NSStackView *stack=[NSStackView stackViewWithViews:@[]];stack.orientation=NSUserInterfaceLayoutOrientationVertical;stack.alignment=NSLayoutAttributeLeading;stack.spacing=10;stack.translatesAutoresizingMaskIntoConstraints=NO;[vc.view addSubview:stack];
 [NSLayoutConstraint activateConstraints:@[[stack.leadingAnchor constraintEqualToAnchor:vc.view.leadingAnchor constant:14],[stack.trailingAnchor constraintEqualToAnchor:vc.view.trailingAnchor constant:-14],[stack.topAnchor constraintEqualToAnchor:vc.view.topAnchor constant:14],[stack.bottomAnchor constraintEqualToAnchor:vc.view.bottomAnchor constant:-14]]];
 [stack addArrangedSubview:label(@"BurnBar · macOS preview")];
 self.meters=[NSStackView stackViewWithViews:@[]];self.meters.orientation=NSUserInterfaceLayoutOrientationVertical;self.meters.alignment=NSLayoutAttributeLeading;self.meters.spacing=4;[stack addArrangedSubview:self.meters];
 NSScrollView *scroll=[[NSScrollView alloc]init];scroll.hasVerticalScroller=YES;scroll.borderType=NSNoBorder;[scroll.heightAnchor constraintEqualToConstant:285].active=YES;[scroll.widthAnchor constraintEqualToConstant:342].active=YES;
 self.summary=[[NSTextView alloc]initWithFrame:NSMakeRect(0,0,342,285)];self.summary.editable=NO;self.summary.selectable=YES;self.summary.font=[NSFont systemFontOfSize:12];self.summary.autoresizingMask=NSViewWidthSizable;self.summary.textContainer.widthTracksTextView=YES;scroll.documentView=self.summary;[stack addArrangedSubview:scroll];
 NSStackView *buttons=[NSStackView stackViewWithViews:@[button(@"Refresh",self,@selector(refresh:)),button(@"Preferences…",self,@selector(preferences:)),button(@"Close",self,@selector(close:))]];[stack addArrangedSubview:buttons];
 self.popover.contentViewController=vc;
 if(self.smoke){fprintf(stderr,"Native views created; first tick\n");fflush(stderr);}
 [self tick:nil];[NSTimer scheduledTimerWithTimeInterval:1 target:self selector:@selector(tick:) userInfo:nil repeats:YES];
 if(self.smoke)[self performSelector:@selector(smokeStart) withObject:nil afterDelay:0.5];
}
- (void)tick:(id)sender {
 bb_tick();self.state=readState();
 NSString *text=self.automaticNotice ?: self.state[@"summary"] ?: @"Loading…";
 if(![self.summary.string isEqualToString:text])self.summary.string=text;
 // Only rebuild noninteractive meter views, keeping text selection and controls stable.
 if(self.popover.shown||self.smoke){for(NSView *v in self.meters.arrangedSubviews){[self.meters removeArrangedSubview:v];[v removeFromSuperview];}
  if(!self.automaticNotice)for(NSDictionary *m in self.state[@"meters"]){[self.meters addArrangedSubview:label([NSString stringWithFormat:@"%@: %.0f%%",m[@"label"],[m[@"percent"] doubleValue]])];NSProgressIndicator *bar=[[NSProgressIndicator alloc]initWithFrame:NSMakeRect(0,0,330,8)];bar.indeterminate=NO;bar.minValue=0;bar.maxValue=100;bar.doubleValue=[m[@"percent"]doubleValue];[bar.widthAnchor constraintEqualToConstant:330].active=YES;[bar.heightAnchor constraintEqualToConstant:8].active=YES;[self.meters addArrangedSubview:bar];}}
 if(self.deadline&&[self.deadline timeIntervalSinceNow]<=0)[self close:nil];
 NSString *notice=self.state[@"notice"];if(notice.length&&!self.popover.shown&&!self.settings.visible){self.automaticNotice=notice;[self show:NO];}
}
- (void)show:(BOOL)manual {
 if(manual)self.automaticNotice=nil;
 self.summary.string=self.automaticNotice ?: self.state[@"summary"] ?: @"Loading…";
 self.deadline=[NSDate dateWithTimeIntervalSinceNow:[self.state[@"seconds"] doubleValue]];
 [self.popover showRelativeToRect:self.status.button.bounds ofView:self.status.button preferredEdge:NSRectEdgeMinY];
 if(manual)[NSApp activateIgnoringOtherApps:YES];
 [self tick:nil];
}
- (void)clicked:(id)sender {if(NSApp.currentEvent.type==NSEventTypeRightMouseUp){[NSMenu popUpContextMenu:self.menu withEvent:NSApp.currentEvent forView:self.status.button];}else if(self.popover.shown){[self close:nil];}else{[self show:YES];}}
- (void)popoverDidClose:(NSNotification *)notification {self.deadline=nil;self.automaticNotice=nil;}
- (void)close:(id)sender {[self.popover performClose:nil];self.deadline=nil;self.automaticNotice=nil;}
- (void)authorize:(id)sender {bb_action(5);[self show:YES];}
- (void)refresh:(id)sender {bb_action(1);if(self.popover.shown)self.deadline=[NSDate dateWithTimeIntervalSinceNow:[self.state[@"seconds"]doubleValue]];[self tick:nil];}
- (void)snooze:(id)sender {bb_action(2);[self close:nil];}
- (void)resume:(id)sender {bb_action(3);}
- (void)updates:(id)sender {bb_action(4);[self preferences:nil];}
- (void)releases:(id)sender {NSString *url=readState()[@"releaseURL"];[[NSWorkspace sharedWorkspace]openURL:[NSURL URLWithString:url.length?url:@"https://github.com/nooblk-98/burnbar/releases"]];}
- (void)quit:(id)sender {[NSApp terminate:nil];}
- (void)preferences:(id)sender {
 [self close:nil];if(self.settings.visible){[self.settings makeKeyAndOrderFront:nil];return;}
 self.state=readState();NSDictionary *c=self.state[@"config"];
 self.settings=[[NSWindow alloc]initWithContentRect:NSMakeRect(0,0,600,580) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable backing:NSBackingStoreBuffered defer:NO];self.settings.title=@"BurnBar preferences";self.settings.releasedWhenClosed=NO;
 NSStackView *stack=[NSStackView stackViewWithViews:@[]];stack.orientation=NSUserInterfaceLayoutOrientationVertical;stack.alignment=NSLayoutAttributeLeading;stack.spacing=8;stack.translatesAutoresizingMaskIntoConstraints=NO;[self.settings.contentView addSubview:stack];[NSLayoutConstraint activateConstraints:@[[stack.leadingAnchor constraintEqualToAnchor:self.settings.contentView.leadingAnchor constant:20],[stack.topAnchor constraintEqualToAnchor:self.settings.contentView.topAnchor constant:20],[stack.trailingAnchor constraintEqualToAnchor:self.settings.contentView.trailingAnchor constant:-20]]];
 self.fields=[NSMutableDictionary dictionary];
 NSArray *rows=@[@[@"Providers",@"Providers (both / claude / codex)",c[@"providers"]?:@"both"],@[@"Seconds",@"Close after seconds (1–300)",[c[@"popupSeconds"]stringValue]],@[@"Interval",@"Refresh minutes (1–60)",[c[@"intervalMinutes"]stringValue]],@[@"Primary",@"Session thresholds (%)",[c[@"primaryThresholds"]componentsJoinedByString:@", "]],@[@"Secondary",@"Weekly thresholds (%)",[c[@"secondaryThresholds"]componentsJoinedByString:@", "]],@[@"QuietStart",@"Quiet start (HH:MM)",[self clock:c[@"quietStart"]]],@[@"QuietEnd",@"Quiet end (HH:MM)",[self clock:c[@"quietEnd"]]]];
 for(NSArray *row in rows){NSTextField *edit=[NSTextField textFieldWithString:row[2]?:@""];[edit.widthAnchor constraintEqualToConstant:280].active=YES;[edit setAccessibilityLabel:row[1]];self.fields[row[0]]=edit;NSStackView *line=[NSStackView stackViewWithViews:@[label(row[1]),edit]];line.spacing=12;[stack addArrangedSubview:line];}
 for(NSArray *row in @[@[@"Milestones",@"Automatic milestone popovers (experimental)",@"milestonePopups"],@[@"QuietHours",@"Enable quiet hours",@"quietHours"],@[@"Forecast",@"Show recent-pace estimates",@"showForecast"],@[@"Updates",@"Check public stable releases daily",@"checkUpdates"]]){NSButton *check=[NSButton checkboxWithTitle:row[1] target:nil action:nil];check.state=[c[row[2]]boolValue]?NSControlStateValueOn:NSControlStateValueOff;self.fields[row[0]]=check;[stack addArrangedSubview:check];}
 [stack addArrangedSubview:label(@"Preview: Focus, startup and global shortcuts are not yet integrated.")];
 [stack addArrangedSubview:label(self.state[@"update"]?:@"")];
 [stack addArrangedSubview:label(@"Update: npx --yes burnbar@development update")];
 NSButton *save=button(@"Save",self,@selector(save:));save.keyEquivalent=@"\r";NSButton *cancel=button(@"Cancel",self,@selector(cancel:));cancel.keyEquivalent=@"\e";[stack addArrangedSubview:[NSStackView stackViewWithViews:@[save,cancel,button(@"Release notes",self,@selector(releases:))]]];
 [self.settings center];[self.settings makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
}
- (NSString *)clock:(NSNumber *)n {return [NSString stringWithFormat:@"%02ld:%02ld",(long)n.integerValue/60,(long)n.integerValue%60];}
- (void)save:(id)sender {
 NSMutableDictionary *data=[NSMutableDictionary dictionary];
 for(NSString *key in self.fields){NSControl *control=self.fields[key];if([control isKindOfClass:NSButton.class])data[key]=[NSNumber numberWithBool:([(NSButton *)control state]==NSControlStateValueOn)];else if([key isEqualToString:@"Seconds"]||[key isEqualToString:@"Interval"]){NSString *text=control.stringValue;NSScanner *scan=[NSScanner scannerWithString:text];NSInteger n;if(![scan scanInteger:&n]||!scan.isAtEnd){[self error:@"Enter whole numbers for seconds and refresh minutes"];return;}data[key]=@(n);}else data[key]=control.stringValue;}
 NSData *json=[NSJSONSerialization dataWithJSONObject:data options:0 error:nil];NSString *raw=[[NSString alloc]initWithData:json encoding:NSUTF8StringEncoding];if(self.smoke){fprintf(stderr,"Synthetic preferences: %s\n",raw.UTF8String);fflush(stderr);}
 char *err=bb_save((char *)raw.UTF8String);
 if(err){NSString *text=[NSString stringWithUTF8String:err];free(err);[self error:text];return;}
 [self.settings close];[self tick:nil];
}
- (void)error:(NSString *)text {if(self.smoke){fprintf(stderr,"Preferences validation failed: %s\n",text.UTF8String);fflush(stderr);exit(1);}NSAlert *alert=[[NSAlert alloc]init];alert.messageText=@"Could not save preferences";alert.informativeText=text;[alert runModal];}
- (void)cancel:(id)sender {[self.settings close];}
- (void)snapshot:(NSView *)view name:(NSString *)name {
 [view layoutSubtreeIfNeeded];NSBitmapImageRep *rep=[view bitmapImageRepForCachingDisplayInRect:view.bounds];
 if(!rep){fprintf(stderr,"Native view snapshot unavailable\n");return;}
 [view cacheDisplayInRect:view.bounds toBitmapImageRep:rep];
 NSString *dir=NSProcessInfo.processInfo.environment[@"APPDATA"] ?: NSTemporaryDirectory();
 NSData *data=[rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
 [data writeToFile:[dir stringByAppendingPathComponent:name] atomically:YES];
}
- (void)smokeStart {
 fprintf(stderr,"Checking native controls and preferences\n");fflush(stderr);
 if(!self.status.button||![self.state[@"summary"]containsString:@"Claude usage"]||![self.state[@"summary"]containsString:@"Codex usage"]){fprintf(stderr,"Missing native usage controls\n");exit(1);}
 [self preferences:nil];[self snapshot:self.settings.contentView name:@"macos-preferences.png"];((NSTextField *)self.fields[@"Primary"]).stringValue=@"25, 50, 75, 100";[self cancel:nil];
 NSArray *before=readState()[@"config"][@"primaryThresholds"];if([before count]!=10){fprintf(stderr,"Cancel changed settings\n");exit(1);}
 [self preferences:nil];((NSTextField *)self.fields[@"Primary"]).stringValue=@"25, 50, 75, 100";((NSTextField *)self.fields[@"Seconds"]).stringValue=@"2";[self save:nil];
 NSDictionary *config=readState()[@"config"];if([config[@"primaryThresholds"]count]!=4||[config[@"popupSeconds"]intValue]!=2){fprintf(stderr,"Save failed\n");exit(1);}
 fprintf(stderr,"Preferences Save/Cancel checked\n");fflush(stderr);
 [self show:YES];if(!self.popover.shown||self.meters.arrangedSubviews.count<4){fprintf(stderr,"Popover did not open\n");exit(1);}
 [self snapshot:self.popover.contentViewController.view name:@"macos-popover.png"];
 [self performSelector:@selector(smokeFinish) withObject:nil afterDelay:4];
}
- (void)smokeFinish {if(self.popover.shown){fprintf(stderr,"Popover did not expire\n");exit(1);}fprintf(stdout,"Native macOS controls, Save/Cancel and timed popover passed\n");exit(0);}
@end

int bb_run(int smoke) {
 @autoreleasepool {
 if(!NSThread.isMainThread){fprintf(stderr,"AppKit was not started on the main thread\n");return 1;}
 if(smoke){fprintf(stderr,"Creating NSApplication\n");fflush(stderr);}
 [NSApplication sharedApplication];[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
 static BurnBarDelegate *delegate;delegate=[[BurnBarDelegate alloc]init];delegate.smoke=smoke;NSApp.delegate=delegate;
 if(smoke){fprintf(stderr,"Starting native AppKit smoke\n");fflush(stderr);}
 [NSApp finishLaunching];[NSApp run];
 }
 return 0;
}
