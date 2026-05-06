using Comprehensive.Interfaces;

namespace Comprehensive.Services
{
    public class GreetingService : IService
    {
        public bool IsReady => true;

        public string Execute(string input)
        {
            return $"Hello, {input}!";
        }
    }
}
