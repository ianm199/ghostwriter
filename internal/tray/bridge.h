#ifndef TRAY_BRIDGE_H
#define TRAY_BRIDGE_H

void TrayBridgeSetup(void);
void TrayBridgeRun(void);
void TrayBridgeUpdateStatus(const char *statusText, int state);
void TrayBridgeTogglePanel(void);
void TrayBridgeUpdateTranscripts(const char *jsonData);
void TrayBridgeUpdateEvents(const char *jsonData);
void TrayBridgeShowTranscriptDetail(const char *jsonData);
void TrayBridgeQuit(void);

extern void goTrayOnReady();
extern void goTrayOnStart();
extern void goTrayOnStop();
extern void goTrayOnOpenTranscripts();
extern void goTrayOnTogglePanel();
extern void goTrayOnSelectTranscript(char *transcriptID);
extern void goTrayOnQuit();

#endif
