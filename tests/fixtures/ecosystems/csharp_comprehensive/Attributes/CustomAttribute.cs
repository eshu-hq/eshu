using System;

namespace Comprehensive.Attributes
{
    [AttributeUsage(AttributeTargets.Class | AttributeTargets.Method)]
    public class LoggedAttribute : Attribute
    {
        public string Category { get; }

        public LoggedAttribute(string category = "default")
        {
            Category = category;
        }
    }

    [Logged("service")]
    public class AttributeDemo
    {
        [Logged("method")]
        public void Process()
        {
            // processing
        }
    }
}
