using System;

namespace Comprehensive.Enums
{
    public enum Status
    {
        Active,
        Inactive,
        Pending,
        Deleted
    }

    [Flags]
    public enum Permissions
    {
        None = 0,
        Read = 1,
        Write = 2,
        Execute = 4,
        All = Read | Write | Execute
    }
}
