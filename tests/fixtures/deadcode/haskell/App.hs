module App
  ( main
  , publicHaskellApi
  ) where

main :: IO ()
main = do
  directHaskellHelper
  selectedHaskellHandler
  runCommandRoot

unusedHaskellHelper :: IO ()
unusedHaskellHelper = pure ()

directHaskellHelper :: IO ()
directHaskellHelper = pure ()

publicHaskellApi :: String
publicHaskellApi = "public"

runCommandRoot :: IO ()
runCommandRoot = directHaskellHelper

selectedHaskellHandler :: IO ()
selectedHaskellHandler = directHaskellHelper

generatedHaskellStub :: IO ()
generatedHaskellStub = pure ()

dynamicHaskellDispatch :: String -> IO ()
dynamicHaskellDispatch name =
  case name of
    "direct" -> directHaskellHelper
    _ -> pure ()
