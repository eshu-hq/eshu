#define C_EVENT_LIMIT 64

enum CEventType {
    C_EVENT_CREATED,
    C_EVENT_UPDATED
};

union CEventValue {
    int code;
    float ratio;
};

struct CEvent {
    int id;
    union CEventValue value;
};

struct CEvent* event_next(struct CEvent* event) {
    return event;
}
