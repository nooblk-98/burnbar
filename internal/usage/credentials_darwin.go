//go:build darwin && cgo

package usage

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <stdlib.h>
#include <string.h>
static char *bb_claude_keychain(int prompt) {
 CFMutableDictionaryRef query=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFDictionarySetValue(query,kSecClass,kSecClassGenericPassword);
 CFDictionarySetValue(query,kSecAttrService,CFSTR("Claude Code-credentials"));
 CFDictionarySetValue(query,kSecReturnData,kCFBooleanTrue);
 CFDictionarySetValue(query,kSecMatchLimit,kSecMatchLimitOne);
 CFDictionarySetValue(query,kSecUseAuthenticationUI,prompt?kSecUseAuthenticationUIAllow:kSecUseAuthenticationUIFail);
 CFTypeRef data=NULL;OSStatus status=SecItemCopyMatching(query,&data);CFRelease(query);
 if(status!=errSecSuccess||!data)return NULL;
 if(CFGetTypeID(data)!=CFDataGetTypeID()){CFRelease(data);return NULL;}
 CFIndex n=CFDataGetLength(data);char *result=malloc(n+1);
 if(result){memcpy(result,CFDataGetBytePtr(data),n);result[n]=0;}
 CFRelease(data);return result;
}
*/
import "C"
import (
	"fmt"
	"os"
	"sync/atomic"
	"unsafe"
)

var keychainPrompt atomic.Bool

// AllowClaudeKeychain is called only after a deliberate authorization action.
func AllowClaudeKeychain() { keychainPrompt.Store(true) }
func claudeCredentials() ([]byte, error) {
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		b, e := os.ReadFile(credentialsPath())
		if e != nil {
			return nil, fmt.Errorf("custom Claude profile has no readable credentials file; custom-profile Keychain entries are not yet supported")
		}
		return b, nil
	}
	prompt := 0
	if keychainPrompt.Swap(false) {
		prompt = 1
	}
	raw := C.bb_claude_keychain(C.int(prompt))
	if raw != nil {
		defer C.free(unsafe.Pointer(raw))
		return []byte(C.GoString(raw)), nil
	}
	if b, e := os.ReadFile(credentialsPath()); e == nil {
		return b, nil
	}
	return nil, fmt.Errorf("sign in with claude, then choose Authorize Claude Keychain from BurnBar's menu; background reads never prompt")
}
