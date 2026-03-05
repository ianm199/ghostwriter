#ifndef TRAY_BRIDGE_H
#define TRAY_BRIDGE_H

void TrayBridgeRun(void);
void TrayBridgeUpdateStatus(const char *statusText, int state);
void TrayBridgeQuit(void);

extern void goTrayOnReady();
extern void goTrayOnStart();
extern void goTrayOnStop();
extern void goTrayOnOpenTranscripts();
extern void goTrayOnQuit();

#endif
