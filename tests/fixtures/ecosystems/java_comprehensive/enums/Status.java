package comprehensive.enums;

/**
 * Status enum with abstract method.
 */
public enum Status {
    ACTIVE {
        @Override
        public boolean isTerminal() {
            return false;
        }
    },
    INACTIVE {
        @Override
        public boolean isTerminal() {
            return false;
        }
    },
    DELETED {
        @Override
        public boolean isTerminal() {
            return true;
        }
    };

    public abstract boolean isTerminal();
}
