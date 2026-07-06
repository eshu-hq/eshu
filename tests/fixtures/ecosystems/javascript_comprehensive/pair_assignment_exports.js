/**
 * Object-literal ("pair") and bare-assignment function-valued export patterns
 * for parser/pre-scan equivalence testing.
 */

const handlers = {
    onStart() {
        return "started";
    },
    onStop: function onStop() {
        return "stopped";
    },
    onTick: () => {
        return "ticked";
    },
};

module.exports.encode = function encode(data) {
    return String(data);
};

exports.decode = function decode(data) {
    return JSON.parse(data);
};

module.exports = handlers;
