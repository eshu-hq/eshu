using System;

namespace DeadCodeFixture;

public interface IJob
{
    void Run();
}

public sealed class ReportJob : IJob
{
    public void Run()
    {
        DirectlyUsedHelper();
    }

    private static void DirectlyUsedHelper()
    {
        Console.WriteLine("used");
    }

    private static void UnusedCleanupCandidate()
    {
        Console.WriteLine("unused");
    }
}

public sealed class PublicController
{
    public string Get() => "ok";
}

public sealed class Worker
{
    [Obsolete("fixture framework root")]
    public void ExecuteAsync() => Console.WriteLine("worker");
}

public static class GeneratedFile
{
    public static void GeneratedExcludedHelper() => Console.WriteLine("generated");
}

public static class Program
{
    public const string DynamicActionName = "Run";

    public static void Main()
    {
        IJob job = new ReportJob();
        job.Run();
        new PublicController().Get();
    }
}
