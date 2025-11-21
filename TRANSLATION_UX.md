# Translation UX Options

## Current Implementation (Separate Message)

The plugin currently posts translations as separate messages below the original:

```
User: 안녕하세요
autotranslate-bot: [ko → en] Hello
```

**Pros:**
- ✅ Simple server-only implementation
- ✅ Works with all Mattermost versions
- ✅ No webapp code required

**Cons:**
- ⚠️ Creates additional messages in the channel

---

## Alternative: Inline Translation Display

To display translations **inline below the original message** (like Slack's translation feature), you need to add a **webapp plugin component**.

### Option 1: Message Attachments (Server-Only)

Use Mattermost's built-in attachments to show translation below the message:

```go
translatedPost := &model.Post{
    ChannelId: post.ChannelId,
    UserId:    post.UserId,
    RootId:    post.RootId,
    Message:   "", // Empty message
    Props: map[string]interface{}{
        "from_plugin": true,
        "attachments": []interface{}{
            map[string]interface{}{
                "pretext": fmt.Sprintf("🌐 **Translation** [%s → %s]", sourceLang, targetLang),
                "text":    translatedText,
                "color":   "#3AA3E3",
            },
        },
    },
}
```

**Result:**
```
User: 안녕하세요
┌────────────────────────────┐
│ 🌐 Translation [ko → en]   │
│ Hello                       │
└────────────────────────────┘
```

### Option 2: Webapp Plugin (Advanced)

Create a webapp plugin to render translations inline:

**File: `webapp/src/components/translated_post.jsx`**
```javascript
import React from 'react';

export default class TranslatedPost extends React.PureComponent {
    render() {
        const {post} = this.props;
        const translation = post.props.translation;

        if (!translation) {
            return null;
        }

        return (
            <div className="post-message--translation">
                <div className="translation-header">
                    🌐 {translation.sourceLang} → {translation.targetLang}
                </div>
                <div className="translation-text">
                    {translation.text}
                </div>
            </div>
        );
    }
}
```

**Register in `webapp/src/index.js`:**
```javascript
import TranslatedPost from './components/translated_post';

class Plugin {
    initialize(registry) {
        registry.registerPostWillRenderEmbedComponent(
            (post) => post.props && post.props.translation,
            TranslatedPost
        );
    }
}

window.registerPlugin('autotranslate', new Plugin());
```

**Update server to include translation in props:**
```go
translatedPost := &model.Post{
    ChannelId: post.ChannelId,
    UserId:    post.UserId,
    RootId:    post.RootId,
    Message:   botMessage,
    Props: map[string]interface{}{
        "from_plugin": true,
        "translation": map[string]interface{}{
            "text":       translatedText,
            "sourceLang": sourceLangDisplay,
            "targetLang": userInfo.TargetLanguage,
        },
    },
}
```

---

## Recommendation

**For most users:** Use the current implementation (separate messages)
- Simple and reliable
- No additional development required

**For advanced UX:** Implement Option 1 (Message Attachments)
- Server-only changes
- Better visual appearance
- Still simple to maintain

**For best UX:** Implement Option 2 (Webapp Plugin)
- Professional inline display
- Requires webapp development
- More complex maintenance

---

## Implementation Status

- [x] Server plugin with auto-translation
- [x] Infinite loop prevention
- [x] Support for AWS, vLLM, LiteLLM
- [ ] Message attachments display (optional)
- [ ] Webapp inline display (optional)
